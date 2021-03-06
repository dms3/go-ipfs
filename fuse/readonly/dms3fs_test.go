// +build !nofuse

package readonly

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"sync"
	"testing"

    core "github.com/dms3-fs/go-dms3-fs/core"
    coreunix "github.com/dms3-fs/go-dms3-fs/core/coreunix"
    coremock "github.com/dms3-fs/go-dms3-fs/core/mock"
    dag "github.com/dms3-fs/go-merkledag"
    importer "github.com/dms3-fs/go-unixfs/importer"
    uio "github.com/dms3-fs/go-unixfs/io"

    fstest "bazil.org/fuse/fs/fstestutil"
    chunker "github.com/dms3-fs/go-fs-chunker"
    u "github.com/dms3-fs/go-fs-util"
    dms3ld "github.com/dms3-fs/go-ld-format"
    ci "github.com/dms3-p2p/go-testutil/ci"
)

func maybeSkipFuseTests(t *testing.T) {
	if ci.NoFuse() {
		t.Skip("Skipping FUSE tests")
	}
}

func randObj(t *testing.T, nd *core.Dms3FsNode, size int64) (dms3ld.Node, []byte) {
	buf := make([]byte, size)
	u.NewTimeSeededRand().Read(buf)
	read := bytes.NewReader(buf)
	obj, err := importer.BuildTrickleDagFromReader(nd.DAG, chunker.DefaultSplitter(read))
	if err != nil {
		t.Fatal(err)
	}

	return obj, buf
}

func setupDms3FsTest(t *testing.T, node *core.Dms3FsNode) (*core.Dms3FsNode, *fstest.Mount) {
	maybeSkipFuseTests(t)

	var err error
	if node == nil {
		node, err = coremock.NewMockNode()
		if err != nil {
			t.Fatal(err)
		}
	}

	fs := NewFileSystem(node)
	mnt, err := fstest.MountedT(t, fs, nil)
	if err != nil {
		t.Fatal(err)
	}

	return node, mnt
}

// Test writing an object and reading it back through fuse
func TestDms3FsBasicRead(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	nd, mnt := setupDms3FsTest(t, nil)
	defer mnt.Close()

	fi, data := randObj(t, nd, 10000)
	k := fi.Cid()
	fname := path.Join(mnt.Dir, k.String())
	rbuf, err := ioutil.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(rbuf, data) {
		t.Fatal("Incorrect Read!")
	}
}

func getPaths(t *testing.T, dms3fs *core.Dms3FsNode, name string, n *dag.ProtoNode) []string {
	if len(n.Links()) == 0 {
		return []string{name}
	}
	var out []string
	for _, lnk := range n.Links() {
		child, err := lnk.GetNode(dms3fs.Context(), dms3fs.DAG)
		if err != nil {
			t.Fatal(err)
		}

		childpb, ok := child.(*dag.ProtoNode)
		if !ok {
			t.Fatal(dag.ErrNotProtobuf)
		}

		sub := getPaths(t, dms3fs, path.Join(name, lnk.Name), childpb)
		out = append(out, sub...)
	}
	return out
}

// Perform a large number of concurrent reads to stress the system
func TestDms3FsStressRead(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	nd, mnt := setupDms3FsTest(t, nil)
	defer mnt.Close()

	var nodes []dms3ld.Node
	var paths []string

	nobj := 50
	ndiriter := 50

	// Make a bunch of objects
	for i := 0; i < nobj; i++ {
		fi, _ := randObj(t, nd, rand.Int63n(50000))
		nodes = append(nodes, fi)
		paths = append(paths, fi.Cid().String())
	}

	// Now make a bunch of dirs
	for i := 0; i < ndiriter; i++ {
		db := uio.NewDirectory(nd.DAG)
		for j := 0; j < 1+rand.Intn(10); j++ {
			name := fmt.Sprintf("child%d", j)

			err := db.AddChild(nd.Context(), name, nodes[rand.Intn(len(nodes))])
			if err != nil {
				t.Fatal(err)
			}
		}
		newdir, err := db.GetNode()
		if err != nil {
			t.Fatal(err)
		}

		err = nd.DAG.Add(nd.Context(), newdir)
		if err != nil {
			t.Fatal(err)
		}

		nodes = append(nodes, newdir)
		npaths := getPaths(t, nd, newdir.Cid().String(), newdir.(*dag.ProtoNode))
		paths = append(paths, npaths...)
	}

	// Now read a bunch, concurrently
	wg := sync.WaitGroup{}
	errs := make(chan error)

	for s := 0; s < 4; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < 2000; i++ {
				item := paths[rand.Intn(len(paths))]
				fname := path.Join(mnt.Dir, item)
				rbuf, err := ioutil.ReadFile(fname)
				if err != nil {
					errs <- err
				}

				read, err := coreunix.Cat(nd.Context(), nd, item)
				if err != nil {
					errs <- err
				}

				data, err := ioutil.ReadAll(read)
				if err != nil {
					errs <- err
				}

				if !bytes.Equal(rbuf, data) {
					errs <- errors.New("incorrect read")
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Test writing a file and reading it back
func TestDms3FsBasicDirRead(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	nd, mnt := setupDms3FsTest(t, nil)
	defer mnt.Close()

	// Make a 'file'
	fi, data := randObj(t, nd, 10000)

	// Make a directory and put that file in it
	db := uio.NewDirectory(nd.DAG)
	err := db.AddChild(nd.Context(), "actual", fi)
	if err != nil {
		t.Fatal(err)
	}

	d1nd, err := db.GetNode()
	if err != nil {
		t.Fatal(err)
	}

	err = nd.DAG.Add(nd.Context(), d1nd)
	if err != nil {
		t.Fatal(err)
	}

	dirname := path.Join(mnt.Dir, d1nd.Cid().String())
	fname := path.Join(dirname, "actual")
	rbuf, err := ioutil.ReadFile(fname)
	if err != nil {
		t.Fatal(err)
	}

	dirents, err := ioutil.ReadDir(dirname)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirents) != 1 {
		t.Fatal("Bad directory entry count")
	}
	if dirents[0].Name() != "actual" {
		t.Fatal("Bad directory entry")
	}

	if !bytes.Equal(rbuf, data) {
		t.Fatal("Incorrect Read!")
	}
}

// Test to make sure the filesystem reports file sizes correctly
func TestFileSizeReporting(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	nd, mnt := setupDms3FsTest(t, nil)
	defer mnt.Close()

	fi, data := randObj(t, nd, 10000)
	k := fi.Cid()

	fname := path.Join(mnt.Dir, k.String())

	finfo, err := os.Stat(fname)
	if err != nil {
		t.Fatal(err)
	}

	if finfo.Size() != int64(len(data)) {
		t.Fatal("Read incorrect size from stat!")
	}
}
