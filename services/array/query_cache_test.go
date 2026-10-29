package array

import (
	"testing"
	"time"

	pb "github.com/kubemq-io/protobuf/go"
	"github.com/stretchr/testify/require"
)

func TestQueryCacheTTL(t *testing.T) {
	require.Equal(t, 60*time.Second, queryCacheTTL(60))
	require.Equal(t, 999*time.Second, queryCacheTTL(999))
	require.Equal(t, time.Second, queryCacheTTL(1000))
	require.Equal(t, time.Minute, queryCacheTTL(60000))
	require.Equal(t, time.Duration(0), queryCacheTTL(0))
}

func TestQueryCache(t *testing.T) {
	c := newQueryCache()
	req := &pb.Request{RequestID: "r1", Channel: "users", CacheKey: "42", CacheTTL: 1000}
	_, ok := c.get(req)
	require.False(t, ok)

	c.put(req, &pb.Response{RequestID: "r1", Body: []byte("alice"), Executed: true})
	res, ok := c.get(&pb.Request{RequestID: "r2", Channel: "users", CacheKey: "42"})
	require.True(t, ok)
	require.Equal(t, "r2", res.RequestID, "a hit answers the new request")
	require.True(t, res.CacheHit)
	require.Equal(t, []byte("alice"), res.Body)

	_, ok = c.get(&pb.Request{Channel: "users", CacheKey: "43"})
	require.False(t, ok, "other key")
	_, ok = c.get(&pb.Request{Channel: "orders", CacheKey: "42"})
	require.False(t, ok, "other channel")

	failed := &pb.Request{Channel: "users", CacheKey: "44", CacheTTL: 60}
	c.put(failed, &pb.Response{Executed: false, Error: "no such user"})
	_, ok = c.get(failed)
	require.False(t, ok, "failed responses are not cached")

	short := &pb.Request{Channel: "users", CacheKey: "45", CacheTTL: 1001}
	c.put(short, &pb.Response{Executed: true})
	require.Eventually(t, func() bool { _, ok := c.get(short); return !ok }, 3*time.Second, 50*time.Millisecond, "expires")
}
