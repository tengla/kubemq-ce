package jstore

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/kubemq-io/kubemq-community/pkg/pipe"
	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
)

type testBroker struct {
	t    *testing.T
	dir  string
	ns   *natsd.Server
	pipe *pipe.Pipe
}

func startBroker(t *testing.T, dir string) *testBroker {
	ns, err := natsd.NewServer(&natsd.Options{DontListen: true, NoSigs: true, JetStream: true, StoreDir: dir, MaxPayload: 100 << 20, MaxPending: 200 << 20})
	require.NoError(t, err)
	go ns.Start()
	require.True(t, ns.ReadyForConnections(10*time.Second))
	p := pipe.NewPipe(nuid.Next())
	p.Bind(ns, pipe.Limits{})
	b := &testBroker{t: t, dir: dir, ns: ns, pipe: p}
	t.Cleanup(b.stop)
	return b
}

func (b *testBroker) stop() {
	b.ns.Shutdown()
	b.ns.WaitForShutdown()
}

func (b *testBroker) connect() *Conn {
	nc, err := nats.Connect("", nats.InProcessServer(b.pipe))
	require.NoError(b.t, err)
	c, err := Connect(nc, b.pipe, nuid.Next())
	require.NoError(b.t, err)
	b.t.Cleanup(func() {
		_ = c.Close()
		nc.Close()
	})
	return c
}

func publish(t *testing.T, c *Conn, subject string, n int) {
	for i := 1; i <= n; i++ {
		require.NoError(t, c.Publish(subject, []byte(fmt.Sprint(i))))
	}
}

func collect(sub chan *Msg) func(t *testing.T, n int) []*Msg {
	return func(t *testing.T, n int) []*Msg {
		list := make([]*Msg, 0, n)
		for len(list) < n {
			select {
			case m := <-sub:
				list = append(list, m)
			case <-time.After(5 * time.Second):
				require.Failf(t, "timeout", "got %d of %d messages", len(list), n)
			}
		}
		return list
	}
}

func sequences(list []*Msg) []uint64 {
	seqs := make([]uint64, len(list))
	for i, m := range list {
		seqs[i] = m.Sequence
	}
	return seqs
}

func TestClose_HandsBackHeldMessagesInOrder(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 5)

	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), MaxInflight(3), ManualAck())
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, sequences(collect(ch)(t, 3)))
	require.NoError(t, sub.Close())

	ch2 := make(chan *Msg, 10)
	sub2, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch2 <- m }, QueueDurable(), DeliverAll(), MaxInflight(5))
	require.NoError(t, err)
	defer func() { _ = sub2.Close() }()
	require.Equal(t, []uint64{1, 2, 3, 4, 5}, sequences(collect(ch2)(t, 5)))
}

func TestHandBack_KeepsTakenAndFreesTheRest(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 2)

	var mu sync.Mutex
	takes := 2
	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) {
		mu.Lock()
		defer mu.Unlock()
		if takes == 0 {
			_ = m.Release()
			return
		}
		takes--
		ch <- m
	}, QueueDurable(), DeliverAll(), MaxInflight(100), ManualAck())
	require.NoError(t, err)
	taken := collect(ch)(t, 2)
	mu.Lock()
	takes = 0
	mu.Unlock()
	sub.HandBack()

	// Sent after the hand back, so a request left waiting would take them.
	publish(t, c, "_QUEUES_.a", 3)
	ch2 := make(chan *Msg, 10)
	sub2, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch2 <- m }, QueueDurable(), DeliverAll(), MaxInflight(100), ManualAck())
	require.NoError(t, err)
	require.Equal(t, []uint64{3, 4, 5}, sequences(collect(ch2)(t, 3)))
	require.NoError(t, sub2.Close())

	// The first subscriber can still ack what it took, and the rest stay.
	for _, m := range taken {
		require.NoError(t, m.Ack())
	}
	require.NoError(t, sub.Close())
	ch3 := make(chan *Msg, 10)
	sub3, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch3 <- m }, QueueDurable(), DeliverAll(), MaxInflight(100), ManualAck())
	require.NoError(t, err)
	defer func() { _ = sub3.Close() }()
	require.Equal(t, []uint64{3, 4, 5}, sequences(collect(ch3)(t, 3)))
}

func TestHandBack_ReturnsQueuedMessagesInOrder(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 50)

	// Everything after the first waits in the subscription's queue.
	blocked, unblock := make(chan struct{}), make(chan struct{})
	first := true
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) {
		if first {
			first = false
			close(blocked)
			<-unblock
			return
		}
		_ = m.Release()
	}, QueueDurable(), DeliverAll(), MaxInflight(50), ManualAck())
	require.NoError(t, err)
	<-blocked
	require.Eventually(t, func() bool {
		sub.mu.Lock()
		defer sub.mu.Unlock()
		return len(sub.queue) == 49
	}, 5*time.Second, 10*time.Millisecond)
	close(unblock)
	sub.HandBack()
	defer func() { _ = sub.Close() }()

	ch := make(chan *Msg, 100)
	sub2, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), MaxInflight(100), ManualAck())
	require.NoError(t, err)
	defer func() { _ = sub2.Close() }()
	want := make([]uint64, 49)
	for i := range want {
		want[i] = uint64(i + 2)
	}
	require.Equal(t, want, sequences(collect(ch)(t, 49)))
}

func TestAck_IsForGood(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 3)

	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), MaxInflight(1), ManualAck())
	require.NoError(t, err)
	m := collect(ch)(t, 1)[0]
	require.EqualValues(t, 1, m.Sequence)
	require.NoError(t, m.Ack())
	require.EqualValues(t, 2, collect(ch)(t, 1)[0].Sequence)
	require.NoError(t, sub.Close())
	require.ErrorIs(t, m.Ack(), ErrSubscriptionClosed)

	ch2 := make(chan *Msg, 10)
	sub2, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch2 <- m }, QueueDurable(), DeliverAll())
	require.NoError(t, err)
	defer func() { _ = sub2.Close() }()
	require.Equal(t, []uint64{2, 3}, sequences(collect(ch2)(t, 2)))
}

func TestHeldMessages_StayHeldPastAckWait(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	ackWait, renewInterval = time.Second, 200*time.Millisecond
	defer func() { ackWait, renewInterval = 30*time.Second, 10*time.Second }()
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 1)

	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), ManualAck())
	require.NoError(t, err)
	defer func() { _ = sub.Close() }()
	collect(ch)(t, 1)

	other := make(chan *Msg, 10)
	sub2, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { other <- m }, QueueDurable(), DeliverAll(), ManualAck())
	require.NoError(t, err)
	defer func() { _ = sub2.Close() }()
	select {
	case <-other:
		require.Fail(t, "a held message went to another subscriber")
	case <-time.After(3 * time.Second):
	}
}

func TestRestart_KeepsMessagesInOrder(t *testing.T) {
	dir := t.TempDir()
	b := startBroker(t, dir)
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 4)

	ch := make(chan *Msg, 10)
	_, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), MaxInflight(3), ManualAck())
	require.NoError(t, err)
	list := collect(ch)(t, 3)
	require.NoError(t, list[0].Ack())
	require.NoError(t, c.nc.Flush())
	time.Sleep(100 * time.Millisecond)
	// As in a crash: seq 2 and 3 are still held, and nobody hands them back.
	b.stop()

	b2 := startBroker(t, dir)
	c2 := b2.connect()
	js, err := jetstream.New(c2.nc)
	require.NoError(t, err)
	require.NoError(t, ResetQueueConsumers(context.Background(), js))
	ch2 := make(chan *Msg, 10)
	sub2, err := c2.Subscribe("_QUEUES_.a", func(m *Msg) { ch2 <- m }, QueueDurable(), DeliverAll())
	require.NoError(t, err)
	defer func() { _ = sub2.Close() }()
	require.Equal(t, []uint64{2, 3, 4}, sequences(collect(ch2)(t, 3)))
}

func TestStopFetching_LeavesTheRestInTheChannel(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 3)
	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll(), MaxInflight(2), ManualAck())
	require.NoError(t, err)
	list := collect(ch)(t, 2)
	sub.StopFetching()
	require.NoError(t, list[0].Ack())
	require.NoError(t, list[1].Ack())
	select {
	case m := <-ch:
		require.Failf(t, "fetched after stopping", "got sequence %d", m.Sequence)
	case <-time.After(300 * time.Millisecond):
	}
	require.NoError(t, sub.Close())
}

func TestDurable_KeepsPositionAndIgnoresNewStart(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "e.a", 3)

	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("e.a", func(m *Msg) { ch <- m }, Durable("client|x"), DeliverAll())
	require.NoError(t, err)
	require.Equal(t, []uint64{1, 2, 3}, sequences(collect(ch)(t, 3)))
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, sub.Close())
	publish(t, c, "e.a", 1)

	sub, err = c.Subscribe("e.a", func(m *Msg) { ch <- m }, Durable("client|x"), StartAtSequence(1))
	require.NoError(t, err)
	require.Equal(t, []uint64{4}, sequences(collect(ch)(t, 1)))
	require.NoError(t, sub.Unsubscribe())

	sub, err = c.Subscribe("e.a", func(m *Msg) { ch <- m }, Durable("client|x"), DeliverLast())
	require.NoError(t, err)
	defer func() { _ = sub.Close() }()
	require.Equal(t, []uint64{4}, sequences(collect(ch)(t, 1)))
}

func TestSubscription_SurvivesChannelDeletion(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	publish(t, c, "_QUEUES_.a", 1)
	ch := make(chan *Msg, 10)
	sub, err := c.Subscribe("_QUEUES_.a", func(m *Msg) { ch <- m }, QueueDurable(), DeliverAll())
	require.NoError(t, err)
	defer func() { _ = sub.Close() }()
	collect(ch)(t, 1)

	acc := b.connect()
	jsCtx, err := acc.nc.JetStream()
	require.NoError(t, err)
	require.NoError(t, jsCtx.DeleteStream(StreamName("_QUEUES_.a")))
	require.Eventually(t, func() bool {
		return c.Publish("_QUEUES_.a", []byte("again")) == nil
	}, 5*time.Second, 50*time.Millisecond)
	require.EqualValues(t, 1, collect(ch)(t, 1)[0].Sequence)
}

func TestPublish_MessageSizes(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	require.NoError(t, c.Publish("_QUEUES_.a", make([]byte, 31<<20)))
	err := c.Publish("_QUEUES_.a", make([]byte, 40<<20))
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMessageTooLarge)
}

func TestPublishBatch_KeepsOrderAndCreatesChannels(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	c := b.connect()
	items := make([]Item, 0, 5000)
	for i := 0; i < 5000; i++ {
		items = append(items, Item{Subject: fmt.Sprintf("q.%d", i%2), Data: []byte(fmt.Sprint(i))})
	}
	for _, err := range c.PublishBatch(items) {
		require.NoError(t, err)
	}
	ch := make(chan *Msg, 3000)
	sub, err := c.Subscribe("q.0", func(m *Msg) { ch <- m }, DeliverAll())
	require.NoError(t, err)
	defer func() { _ = sub.Close() }()
	for i, m := range collect(ch)(t, 2500) {
		require.Equal(t, fmt.Sprint(i*2), string(m.Data))
	}
}

func TestConnect_ClientIDs(t *testing.T) {
	t.Cleanup(leaktest.Check(t))
	b := startBroker(t, t.TempDir())
	nc, err := nats.Connect("", nats.InProcessServer(b.pipe))
	require.NoError(t, err)
	defer nc.Close()

	_, err = Connect(nc, b.pipe, "not valid")
	require.ErrorIs(t, err, ErrInvalidClient)
	c, err := Connect(nc, b.pipe, "same")
	require.NoError(t, err)
	_, err = Connect(nc, b.pipe, "same")
	require.ErrorIs(t, err, ErrClientIDInUse)
	require.NoError(t, c.Close())
	c, err = Connect(nc, b.pipe, "same")
	require.NoError(t, err)
	require.NoError(t, c.Close())
}
