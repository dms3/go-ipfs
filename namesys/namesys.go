package namesys

import (
	"context"
	"strings"
	"time"

	opts "github.com/dms3-fs/go-dms3-fs/namesys/opts"
	path "github.com/dms3-fs/go-path"

	lru "github.com/hashicorp/golang-lru"
	ds "github.com/dms3-fs/go-datastore"
	isd "github.com/jbenet/go-is-domain"
	ci "github.com/dms3-p2p/go-p2p-crypto"
	peer "github.com/dms3-p2p/go-p2p-peer"
	routing "github.com/dms3-p2p/go-p2p-routing"
	mh "github.com/dms3-mft/go-multihash"
)

// mpns (a multi-protocol NameSystem) implements generic DMS3FS naming.
//
// Uses several Resolvers:
// (a) DMS3FS routing naming: SFS-like PKI names.
// (b) dns domains: resolves using links in DNS TXT records
// (c) proquints: interprets string as the raw byte data.
//
// It can only publish to: (a) DMS3FS routing naming.
//
type mpns struct {
	dnsResolver, proquintResolver, dms3nsResolver resolver
	dms3nsPublisher                               Publisher

	cache *lru.Cache
}

// NewNameSystem will construct the DMS3FS naming system based on Routing
func NewNameSystem(r routing.ValueStore, ds ds.Datastore, cachesize int) NameSystem {
	var cache *lru.Cache
	if cachesize > 0 {
		cache, _ = lru.New(cachesize)
	}

	return &mpns{
		dnsResolver:      NewDNSResolver(),
		proquintResolver: new(ProquintResolver),
		dms3nsResolver:     NewDms3NsResolver(r),
		dms3nsPublisher:    NewDms3NsPublisher(r, ds),
		cache:            cache,
	}
}

const DefaultResolverCacheTTL = time.Minute

// Resolve implements Resolver.
func (ns *mpns) Resolve(ctx context.Context, name string, options ...opts.ResolveOpt) (path.Path, error) {
	if strings.HasPrefix(name, "/dms3fs/") {
		return path.ParsePath(name)
	}

	if !strings.HasPrefix(name, "/") {
		return path.ParsePath("/dms3fs/" + name)
	}

	return resolve(ctx, ns, name, opts.ProcessOpts(options), "/dms3ns/")
}

// resolveOnce implements resolver.
func (ns *mpns) resolveOnce(ctx context.Context, name string, options *opts.ResolveOpts) (path.Path, time.Duration, error) {
	if !strings.HasPrefix(name, "/dms3ns/") {
		name = "/dms3ns/" + name
	}
	segments := strings.SplitN(name, "/", 4)
	if len(segments) < 3 || segments[0] != "" {
		log.Debugf("invalid name syntax for %s", name)
		return "", 0, ErrResolveFailed
	}

	key := segments[2]

	p, ok := ns.cacheGet(key)
	var err error
	if !ok {
		// Resolver selection:
		// 1. if it is a multihash resolve through "dms3ns".
		// 2. if it is a domain name, resolve through "dns"
		// 3. otherwise resolve through the "proquint" resolver
		var res resolver
		if _, err := mh.FromB58String(key); err == nil {
			res = ns.dms3nsResolver
		} else if isd.IsDomain(key) {
			res = ns.dnsResolver
		} else {
			res = ns.proquintResolver
		}

		var ttl time.Duration
		p, ttl, err = res.resolveOnce(ctx, key, options)
		if err != nil {
			return "", 0, ErrResolveFailed
		}
		ns.cacheSet(key, p, ttl)
	}

	if len(segments) > 3 {
		p, err = path.FromSegments("", strings.TrimRight(p.String(), "/"), segments[3])
	}
	return p, 0, err
}

// Publish implements Publisher
func (ns *mpns) Publish(ctx context.Context, name ci.PrivKey, value path.Path) error {
	return ns.PublishWithEOL(ctx, name, value, time.Now().Add(DefaultRecordTTL))
}

func (ns *mpns) PublishWithEOL(ctx context.Context, name ci.PrivKey, value path.Path, eol time.Time) error {
	id, err := peer.IDFromPrivateKey(name)
	if err != nil {
		return err
	}
	if err := ns.dms3nsPublisher.PublishWithEOL(ctx, name, value, eol); err != nil {
		return err
	}
	ttl := DefaultResolverCacheTTL
	if ttEol := eol.Sub(time.Now()); ttEol < ttl {
		ttl = ttEol
	}
	ns.cacheSet(peer.IDB58Encode(id), value, ttl)
	return nil
}
