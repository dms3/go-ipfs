package coreapi

import (
	"context"
	"io"

	coreiface "github.com/dms3-fs/go-dms3-fs/core/coreapi/interface"
	coreunix "github.com/dms3-fs/go-dms3-fs/core/coreunix"
	uio "github.com/dms3-fs/go-unixfs/io"

	cid "github.com/dms3-fs/go-cid"
	dms3ld "github.com/dms3-fs/go-ld-format"
)

type UnixfsAPI CoreAPI

// Add builds a merkledag node from a reader, adds it to the blockstore,
// and returns the key representing that node.
func (api *UnixfsAPI) Add(ctx context.Context, r io.Reader) (coreiface.ResolvedPath, error) {
	k, err := coreunix.AddWithContext(ctx, api.node, r)
	if err != nil {
		return nil, err
	}
	c, err := cid.Decode(k)
	if err != nil {
		return nil, err
	}
	return coreiface.Dms3FsPath(c), nil
}

// Cat returns the data contained by an DMS3FS or DMS3NS object(s) at path `p`.
func (api *UnixfsAPI) Cat(ctx context.Context, p coreiface.Path) (coreiface.Reader, error) {
	dget := api.node.DAG // TODO: use a session here once routing perf issues are resolved

	dagnode, err := resolveNode(ctx, dget, api.node.Namesys, p)
	if err != nil {
		return nil, err
	}

	r, err := uio.NewDagReader(ctx, dagnode, dget)
	if err == uio.ErrIsDir {
		return nil, coreiface.ErrIsDir
	} else if err != nil {
		return nil, err
	}
	return r, nil
}

// Ls returns the contents of an DMS3FS or DMS3NS object(s) at path p, with the format:
// `<link base58 hash> <link size in bytes> <link name>`
func (api *UnixfsAPI) Ls(ctx context.Context, p coreiface.Path) ([]*dms3ld.Link, error) {
	dagnode, err := api.core().ResolveNode(ctx, p)
	if err != nil {
		return nil, err
	}

	var ndlinks []*dms3ld.Link
	dir, err := uio.NewDirectoryFromNode(api.node.DAG, dagnode)
	switch err {
	case nil:
		l, err := dir.Links(ctx)
		if err != nil {
			return nil, err
		}
		ndlinks = l
	case uio.ErrNotADir:
		ndlinks = dagnode.Links()
	default:
		return nil, err
	}

	links := make([]*dms3ld.Link, len(ndlinks))
	for i, l := range ndlinks {
		links[i] = &dms3ld.Link{Name: l.Name, Size: l.Size, Cid: l.Cid}
	}
	return links, nil
}

func (api *UnixfsAPI) core() coreiface.CoreAPI {
	return (*CoreAPI)(api)
}
