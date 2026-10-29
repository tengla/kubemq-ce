package array

import (
	"sync"
	"time"

	pb "github.com/kubemq-io/protobuf/go"
)

// queryCacheMaxEntries bounds the cache. When it is full of unexpired
// responses, new ones are not cached until some expire.
const queryCacheMaxEntries = 10000

// queryCache keeps the responses to queries that set a CacheKey, so a later
// query on the same channel with the same key gets the stored response,
// marked CacheHit, without reaching a subscriber.
type queryCache struct {
	mu    sync.Mutex
	items map[string]*queryCacheItem
}

type queryCacheItem struct {
	res     *pb.Response
	expires time.Time
}

func newQueryCache() *queryCache {
	return &queryCache{items: map[string]*queryCacheItem{}}
}

func queryCacheKey(req *pb.Request) string {
	return req.Channel + "\x00" + req.CacheKey
}

// queryCacheTTL reads a request's CacheTTL. The SDKs disagree on its unit:
// kubemq-js and the other SDKs send seconds, kubemq-go milliseconds. Values
// under 1000 are taken as seconds and larger ones as milliseconds, which
// reads short TTLs from either correctly; a TTL of 1000 seconds or more from
// a seconds-based SDK comes out too short.
func queryCacheTTL(v int32) time.Duration {
	if v < 1000 {
		return time.Duration(v) * time.Second
	}
	return time.Duration(v) * time.Millisecond
}

// get returns a copy of the cached response for req, answering req.
func (c *queryCache) get(req *pb.Request) (*pb.Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := queryCacheKey(req)
	item, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(item.expires) {
		delete(c.items, key)
		return nil, false
	}
	res := *item.res
	res.RequestID = req.RequestID
	res.CacheHit = true
	return &res, true
}

// put caches an executed response for its TTL.
func (c *queryCache) put(req *pb.Request, res *pb.Response) {
	ttl := queryCacheTTL(req.CacheTTL)
	if ttl <= 0 || res == nil || !res.Executed {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.items) >= queryCacheMaxEntries {
		for key, item := range c.items {
			if now.After(item.expires) {
				delete(c.items, key)
			}
		}
		if len(c.items) >= queryCacheMaxEntries {
			return
		}
	}
	stored := *res
	c.items[queryCacheKey(req)] = &queryCacheItem{res: &stored, expires: now.Add(ttl)}
}
