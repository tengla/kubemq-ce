package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/kubemq-io/protobuf/go"
)

const (
	kb = 1024
	mb = 1024 * kb

	// settle is how long a receiver may go without a message before what's
	// missing counts as lost.
	settle = 3 * time.Second
	// maxSeed limits the backlog of scenarios that measure draining it.
	maxSeed        = 100000
	maxSeedPerCall = 16 * mb
	scenarioLimit  = 5 * time.Minute

	internalRequests = "kubemq.cluster.internal.requests"
)

type scenario struct {
	name string
	run  func(e *env) (Sample, error)
}

func sizeName(size int) string {
	switch {
	case size >= mb:
		return fmt.Sprintf("%dMB", size/mb)
	case size >= kb:
		return fmt.Sprintf("%dKB", size/kb)
	}
	return fmt.Sprintf("%dB", size)
}

func scenarios() []scenario {
	var list []scenario
	add := func(run func(e *env) (Sample, error), format string, args ...any) {
		list = append(list, scenario{name: fmt.Sprintf(format, args...), run: run})
	}

	// How sending scales at one size, then how it depends on size.
	sends := []struct{ size, workers int }{{kb, 1}, {kb, 8}, {kb, 64}, {100, 8}, {64 * kb, 8}, {mb, 8}}
	for _, c := range sends {
		add(func(e *env) (Sample, error) { return queueSend(e, c.size, c.workers) }, "queue/send/%s/x%d", sizeName(c.size), c.workers)
	}
	for _, per := range []int{100, 1000} {
		add(func(e *env) (Sample, error) { return queueBatch(e, kb, 4, per) }, "queue/batch/1KB/%d-per-call/x4", per)
	}
	add(func(e *env) (Sample, error) { return queueUpstream(e, kb, 4, 100) }, "queue/upstream/1KB/100-per-call/x4")
	for _, c := range []struct{ size, per, limit int }{{kb, 1, 5000}, {kb, 100, maxSeed}, {kb, 1000, maxSeed}, {100, 100, maxSeed}, {64 * kb, 100, maxSeed}, {mb, 10, maxSeed}} {
		add(func(e *env) (Sample, error) { return queueReceive(e, c.size, c.per, c.limit) }, "queue/receive/%s/%d-per-call", sizeName(c.size), c.per)
	}
	add(func(e *env) (Sample, error) { return queueDownstream(e, kb, 100) }, "queue/downstream/1KB/100-per-get")
	add(func(e *env) (Sample, error) { return queueStream(e, kb, 2000) }, "queue/stream-ack/1KB")
	add(func(e *env) (Sample, error) { return queuePipeline(e, kb, 4, 4, 100) }, "queue/pipeline/1KB/4-senders-4-receivers")

	for _, c := range sends {
		add(func(e *env) (Sample, error) { return storeSend(e, c.size, c.workers) }, "events_store/send/%s/x%d", sizeName(c.size), c.workers)
	}
	add(func(e *env) (Sample, error) { return storeReplay(e, kb) }, "events_store/replay/1KB")
	add(func(e *env) (Sample, error) { return storeLive(e, kb, 8) }, "events_store/live/1KB/x8")

	for _, c := range []struct{ size, workers int }{{kb, 1}, {kb, 8}, {kb, 64}, {mb, 8}} {
		add(func(e *env) (Sample, error) { return eventsSend(e, c.size, c.workers) }, "events/send/%s/x%d", sizeName(c.size), c.workers)
	}
	for _, c := range []struct{ size, workers int }{{kb, 1}, {kb, 8}, {kb, 64}} {
		add(func(e *env) (Sample, error) { return requests(e, pb.Request_Command, c.size, c.workers) }, "commands/1KB/x%d", c.workers)
		add(func(e *env) (Sample, error) { return requests(e, pb.Request_Query, c.size, c.workers) }, "queries/1KB/x%d", c.workers)
	}

	add(func(e *env) (Sample, error) { return manyChannelsCreate(e, 1000) }, "channels/create-1000")
	add(func(e *env) (Sample, error) { return manyChannelsSend(e, kb, 8, 1000) }, "channels/send-across-1000/1KB/x8")
	return list
}

func (e *env) channel(name string) string {
	return fmt.Sprintf("bench.%s.%s.%d", e.run, name, channelCounter.Add(1))
}

var channelCounter atomic.Int64

func (e *env) clientID(name string) string {
	return fmt.Sprintf("bench-%s-%d", name, channelCounter.Add(1))
}

// deleteChannel frees the disk a scenario used, so it doesn't weigh on the next.
func (e *env) deleteChannel(channelType, channel string) {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	_, _ = e.client.SendRequest(ctx, &pb.Request{
		RequestID:       e.clientID("delete"),
		RequestTypeData: pb.Request_Query,
		ClientID:        e.clientID("delete"),
		Channel:         internalRequests,
		Metadata:        "delete-channel",
		Timeout:         30000,
		Tags:            map[string]string{"channel_type": channelType, "channel": channel},
	})
}

// sendCount says how many messages of a size fit the budget, if the time doesn't
// end the scenario first.
func (e *env) sendCount(size int) int64 {
	return e.budget/int64(size) + 1
}

// closedLoop runs workers that each call send and wait for it, until the time or
// the budget ends. It's the load a client sees, so with few workers it measures
// the round trip more than the server.
func (e *env) closedLoop(size, workers, msgsPerCall int, send func(ctx context.Context, worker int) error) Sample {
	ctx, cancel := context.WithTimeout(e.ctx, e.duration)
	defer cancel()
	limit := e.sendCount(size)
	var reserved, sent, failed atomic.Int64
	var firstErr atomic.Value
	recorders := make([]latencies, workers)
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for ctx.Err() == nil && reserved.Add(int64(msgsPerCall)) <= limit {
				t := time.Now()
				if err := send(e.ctx, w); err != nil {
					failed.Add(1)
					firstErr.CompareAndSwap(nil, err.Error())
					if failed.Load() > 100 {
						cancel()
					}
					continue
				}
				sent.Add(int64(msgsPerCall))
				recorders[w].add(time.Since(t))
			}
		}(w)
	}
	wg.Wait()
	sample := finish(start, sent.Load(), size, recorders)
	sample.Failed = failed.Load()
	if message := firstErr.Load(); message != nil {
		sample.Error = message.(string)
	}
	return sample
}

func queueMessage(channel, clientID string, body []byte) *pb.QueueMessage {
	return &pb.QueueMessage{ClientID: clientID, Channel: channel, Body: body}
}

func checkSend(result *pb.SendQueueMessageResult, err error) error {
	if err != nil {
		return err
	}
	if result.IsError {
		return errors.New(result.Error)
	}
	return nil
}

func queueSend(e *env, size, workers int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	body := make([]byte, size)
	clientID := e.clientID("send")
	return e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		return checkSend(e.client.SendQueueMessage(ctx, queueMessage(channel, clientID, body)))
	}), nil
}

func batchRequest(channel, clientID string, body []byte, n int) *pb.QueueMessagesBatchRequest {
	request := &pb.QueueMessagesBatchRequest{BatchID: clientID, Messages: make([]*pb.QueueMessage, n)}
	for i := range request.Messages {
		request.Messages[i] = queueMessage(channel, clientID, body)
	}
	return request
}

func sendBatch(ctx context.Context, e *env, request *pb.QueueMessagesBatchRequest) error {
	response, err := e.client.SendQueueMessagesBatch(ctx, request)
	if err != nil {
		return err
	}
	if response.HaveErrors {
		for _, result := range response.Results {
			if result.IsError {
				return errors.New(result.Error)
			}
		}
		return errors.New("batch had errors")
	}
	return nil
}

func queueBatch(e *env, size, workers, per int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	request := batchRequest(channel, e.clientID("batch"), make([]byte, size), per)
	return e.closedLoop(size, workers, per, func(ctx context.Context, _ int) error {
		return sendBatch(ctx, e, request)
	}), nil
}

func queueUpstream(e *env, size, workers, per int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	streams := make([]pb.Kubemq_QueuesUpstreamClient, workers)
	for w := range streams {
		stream, err := e.client.QueuesUpstream(ctx)
		if err != nil {
			return Sample{}, err
		}
		streams[w] = stream
	}
	messages := batchRequest(channel, e.clientID("upstream"), make([]byte, size), per).Messages
	return e.closedLoop(size, workers, per, func(_ context.Context, w int) error {
		if err := streams[w].Send(&pb.QueuesUpstreamRequest{RequestID: "bench", Messages: messages}); err != nil {
			return err
		}
		response, err := streams[w].Recv()
		if err != nil {
			return err
		}
		if response.IsError {
			return errors.New(response.Error)
		}
		return nil
	}), nil
}

// seedQueue fills a channel without being measured, and returns the count.
func seedQueue(e *env, channel string, size, limit int) (int, error) {
	count := min(int(e.budget)/size, maxSeed, limit)
	count = max(count, 1)
	per := max(min(maxSeedPerCall/size, 1000), 1)
	body := make([]byte, size)
	clientID := e.clientID("seed")
	ctx, cancel := context.WithTimeout(e.ctx, scenarioLimit)
	defer cancel()
	for left := count; left > 0; left -= per {
		if err := sendBatch(ctx, e, batchRequest(channel, clientID, body, min(per, left))); err != nil {
			return 0, fmt.Errorf("seed: %w", err)
		}
	}
	return count, nil
}

func queueReceive(e *env, size, per, limit int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	count, err := seedQueue(e, channel, size, limit)
	if err != nil {
		return Sample{}, err
	}
	var recorder latencies
	clientID := e.clientID("receive")
	got := 0
	ctx, cancel := context.WithTimeout(e.ctx, scenarioLimit)
	defer cancel()
	e.startMeasuring()
	start := time.Now()
	for got < count {
		t := time.Now()
		response, err := e.client.ReceiveQueueMessages(ctx, &pb.ReceiveQueueMessagesRequest{ClientID: clientID, Channel: channel, MaxNumberOfMessages: int32(min(per, count-got)), WaitTimeSeconds: 2})
		if err != nil {
			return Sample{}, err
		}
		if response.IsError {
			return Sample{}, errors.New(response.Error)
		}
		if len(response.Messages) == 0 {
			break
		}
		recorder.add(time.Since(t))
		got += len(response.Messages)
	}
	sample := finish(start, int64(got), size, []latencies{recorder})
	sample.Lost = int64(count - got)
	return sample, nil
}

func queueDownstream(e *env, size, per int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	count, err := seedQueue(e, channel, size, maxSeed)
	if err != nil {
		return Sample{}, err
	}
	ctx, cancel := context.WithTimeout(e.ctx, scenarioLimit)
	defer cancel()
	stream, err := e.client.QueuesDownstream(ctx)
	if err != nil {
		return Sample{}, err
	}
	clientID := e.clientID("downstream")
	var recorder latencies
	got := 0
	e.startMeasuring()
	start := time.Now()
	for got < count {
		t := time.Now()
		err := stream.Send(&pb.QueuesDownstreamRequest{RequestID: clientID, ClientID: clientID, RequestTypeData: pb.QueuesDownstreamRequestType_Get, Channel: channel, MaxItems: int32(min(per, count-got)), WaitTimeout: 2000})
		if err != nil {
			return Sample{}, err
		}
		response, err := stream.Recv()
		if err != nil {
			return Sample{}, err
		}
		if response.IsError || len(response.Messages) == 0 {
			break
		}
		err = stream.Send(&pb.QueuesDownstreamRequest{RequestID: clientID, ClientID: clientID, RequestTypeData: pb.QueuesDownstreamRequestType_AckAll, Channel: channel, RefTransactionId: response.TransactionId})
		if err != nil {
			return Sample{}, err
		}
		recorder.add(time.Since(t))
		got += len(response.Messages)
	}
	sample := finish(start, int64(got), size, []latencies{recorder})
	sample.Lost = int64(count - got)
	return sample, nil
}

// queueStream receives and acks one message per stream, as the stream call works.
func queueStream(e *env, size, limit int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	count, err := seedQueue(e, channel, size, limit)
	if err != nil {
		return Sample{}, err
	}
	var recorder latencies
	got := 0
	e.startMeasuring()
	start := time.Now()
	for got < count {
		t := time.Now()
		if err := receiveOneOnStream(e, channel); err != nil {
			sample := finish(start, int64(got), size, []latencies{recorder})
			sample.Lost = int64(count - got)
			return sample, err
		}
		recorder.add(time.Since(t))
		got++
	}
	return finish(start, int64(got), size, []latencies{recorder}), nil
}

func receiveOneOnStream(e *env, channel string) error {
	ctx, cancel := context.WithTimeout(e.ctx, time.Minute)
	defer cancel()
	stream, err := e.client.StreamQueueMessage(ctx)
	if err != nil {
		return err
	}
	clientID := e.clientID("stream")
	err = stream.Send(&pb.StreamQueueMessagesRequest{RequestID: clientID, ClientID: clientID, StreamRequestTypeData: pb.StreamRequestType_ReceiveMessage, Channel: channel, VisibilitySeconds: 60, WaitTimeSeconds: 5})
	if err != nil {
		return err
	}
	response, err := stream.Recv()
	if err != nil {
		return err
	}
	if response.IsError {
		return errors.New(response.Error)
	}
	err = stream.Send(&pb.StreamQueueMessagesRequest{RequestID: clientID, ClientID: clientID, StreamRequestTypeData: pb.StreamRequestType_AckMessage, Channel: channel, RefSequence: response.Message.Attributes.Sequence})
	if err != nil {
		return err
	}
	response, err = stream.Recv()
	if err != nil {
		return err
	}
	if response.IsError {
		return errors.New(response.Error)
	}
	return nil
}

// queuePipeline sends and receives at once. It measures how fast messages get
// through, counted at the receivers.
func queuePipeline(e *env, size, senders, receivers, per int) (Sample, error) {
	channel := e.channel("q")
	e.later(func() { e.deleteChannel("queues", channel) })
	body := make([]byte, size)
	sendClient := e.clientID("pipeline")
	var sent, received, failed atomic.Int64
	var firstErr atomic.Value
	fail := func(err error) {
		failed.Add(1)
		firstErr.CompareAndSwap(nil, err.Error())
	}
	sendCtx, stopSending := context.WithTimeout(e.ctx, e.duration)
	defer stopSending()
	var sending sync.WaitGroup
	limit := e.sendCount(size)
	start := time.Now()
	for w := 0; w < senders; w++ {
		sending.Add(1)
		go func() {
			defer sending.Done()
			for sendCtx.Err() == nil && sent.Load() < limit {
				if err := checkSend(e.client.SendQueueMessage(e.ctx, queueMessage(channel, sendClient, body))); err != nil {
					fail(err)
					return
				}
				sent.Add(1)
			}
		}()
	}
	recorders := make([]latencies, receivers)
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	var receiving sync.WaitGroup
	for w := 0; w < receivers; w++ {
		receiving.Add(1)
		go func(w int) {
			defer receiving.Done()
			clientID := e.clientID("pipeline-rx")
			for {
				if sendCtx.Err() != nil && (received.Load() >= sent.Load() || time.Since(time.Unix(0, last.Load())) > settle) {
					return
				}
				t := time.Now()
				response, err := e.client.ReceiveQueueMessages(e.ctx, &pb.ReceiveQueueMessagesRequest{ClientID: clientID, Channel: channel, MaxNumberOfMessages: int32(per), WaitTimeSeconds: 1})
				if err != nil {
					fail(err)
					return
				}
				if response.IsError {
					fail(errors.New(response.Error))
					return
				}
				if len(response.Messages) > 0 {
					recorders[w].add(time.Since(t))
					received.Add(int64(len(response.Messages)))
					last.Store(time.Now().UnixNano())
				}
			}
		}(w)
	}
	sending.Wait()
	receiving.Wait()
	sample := finishAt(time.Unix(0, last.Load()), start, received.Load(), size, recorders)
	sample.Lost = sent.Load() - received.Load()
	sample.Failed = failed.Load()
	if message := firstErr.Load(); message != nil {
		sample.Error = message.(string)
	}
	return sample, nil
}

func checkEvent(result *pb.Result, err error) error {
	if err != nil {
		return err
	}
	if !result.Sent {
		return errors.New(result.Error)
	}
	return nil
}

func storeSend(e *env, size, workers int) (Sample, error) {
	channel := e.channel("es")
	e.later(func() { e.deleteChannel("events_store", channel) })
	body := make([]byte, size)
	clientID := e.clientID("send")
	return e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		return checkEvent(e.client.SendEvent(ctx, &pb.Event{ClientID: clientID, Channel: channel, Body: body, Store: true}))
	}), nil
}

// subscriber counts what a subscription delivers.
type subscriber struct {
	received atomic.Int64
	last     atomic.Int64
	done     chan struct{}
	cancel   context.CancelFunc
}

func (e *env) subscribe(request *pb.Subscribe) (*subscriber, error) {
	ctx, cancel := context.WithCancel(e.ctx)
	stream, err := e.client.SubscribeToEvents(ctx, request)
	if err != nil {
		cancel()
		return nil, err
	}
	s := &subscriber{done: make(chan struct{}), cancel: cancel}
	s.last.Store(time.Now().UnixNano())
	go func() {
		defer close(s.done)
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
			s.received.Add(1)
			s.last.Store(time.Now().UnixNano())
		}
	}()
	return s, nil
}

// wait returns when n messages came or none came for the settle time.
func (s *subscriber) wait(n int64) {
	for s.received.Load() < n && time.Since(time.Unix(0, s.last.Load())) < settle {
		select {
		case <-s.done:
			return
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *subscriber) close() {
	s.cancel()
	<-s.done
}

func storeReplay(e *env, size int) (Sample, error) {
	channel := e.channel("es")
	e.later(func() { e.deleteChannel("events_store", channel) })
	body := make([]byte, size)
	clientID := e.clientID("seed")
	count := int64(min(int(e.budget)/size, 50000))
	var next atomic.Int64
	var wg sync.WaitGroup
	var firstErr atomic.Value
	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for next.Add(1) <= count {
				if err := checkEvent(e.client.SendEvent(e.ctx, &pb.Event{ClientID: clientID, Channel: channel, Body: body, Store: true})); err != nil {
					firstErr.CompareAndSwap(nil, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err, ok := firstErr.Load().(error); ok {
		return Sample{}, fmt.Errorf("seed: %w", err)
	}
	e.startMeasuring()
	start := time.Now()
	sub, err := e.subscribe(&pb.Subscribe{SubscribeTypeData: pb.Subscribe_EventsStore, ClientID: e.clientID("replay"), Channel: channel, EventsStoreTypeData: pb.Subscribe_StartFromFirst})
	if err != nil {
		return Sample{}, err
	}
	defer sub.close()
	sub.wait(count)
	sample := finishAt(time.Unix(0, sub.last.Load()), start, sub.received.Load(), size, nil)
	sample.Lost = count - sub.received.Load()
	return sample, nil
}

func storeLive(e *env, size, workers int) (Sample, error) {
	channel := e.channel("es")
	e.later(func() { e.deleteChannel("events_store", channel) })
	sub, err := e.subscribe(&pb.Subscribe{SubscribeTypeData: pb.Subscribe_EventsStore, ClientID: e.clientID("live"), Channel: channel, EventsStoreTypeData: pb.Subscribe_StartNewOnly})
	if err != nil {
		return Sample{}, err
	}
	defer sub.close()
	time.Sleep(200 * time.Millisecond)
	body := make([]byte, size)
	clientID := e.clientID("send")
	sample := e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		return checkEvent(e.client.SendEvent(ctx, &pb.Event{ClientID: clientID, Channel: channel, Body: body, Store: true}))
	})
	sub.wait(sample.Messages)
	sample.Lost = sample.Messages - sub.received.Load()
	return sample, nil
}

// eventsSend sends to one subscriber. Events aren't stored, so a slow subscriber
// can lose them, and that's reported instead of hidden.
func eventsSend(e *env, size, workers int) (Sample, error) {
	channel := e.channel("ev")
	sub, err := e.subscribe(&pb.Subscribe{SubscribeTypeData: pb.Subscribe_Events, ClientID: e.clientID("sub"), Channel: channel})
	if err != nil {
		return Sample{}, err
	}
	defer sub.close()
	time.Sleep(200 * time.Millisecond)
	body := make([]byte, size)
	clientID := e.clientID("send")
	sample := e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		return checkEvent(e.client.SendEvent(ctx, &pb.Event{ClientID: clientID, Channel: channel, Body: body}))
	})
	sub.wait(sample.Messages)
	sample.Lost = sample.Messages - sub.received.Load()
	return sample, nil
}

// requests measures request and reply with responders that answer at once.
func requests(e *env, kind pb.Request_RequestType, size, workers int) (Sample, error) {
	channel := e.channel("rpc")
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	subscribeType := pb.Subscribe_Commands
	if kind == pb.Request_Query {
		subscribeType = pb.Subscribe_Queries
	}
	stream, err := e.client.SubscribeToRequests(ctx, &pb.Subscribe{SubscribeTypeData: subscribeType, ClientID: e.clientID("responder"), Channel: channel})
	if err != nil {
		return Sample{}, err
	}
	responderID := e.clientID("responder")
	var responders sync.WaitGroup
	responders.Add(1)
	go func() {
		defer responders.Done()
		var answers sync.WaitGroup
		defer answers.Wait()
		for {
			request, err := stream.Recv()
			if err != nil {
				return
			}
			answers.Add(1)
			go func() {
				defer answers.Done()
				_, _ = e.client.SendResponse(ctx, &pb.Response{ClientID: responderID, RequestID: request.RequestID, ReplyChannel: request.ReplyChannel, Body: request.Body, Executed: true})
			}()
		}
	}()
	time.Sleep(200 * time.Millisecond)
	body := make([]byte, size)
	clientID := e.clientID("request")
	sample := e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		response, err := e.client.SendRequest(ctx, &pb.Request{RequestID: nextRequestID(), RequestTypeData: kind, ClientID: clientID, Channel: channel, Body: body, Timeout: 10000})
		if err != nil {
			return err
		}
		if !response.Executed {
			return errors.New(response.Error)
		}
		return nil
	})
	cancel()
	responders.Wait()
	return sample, nil
}

var requestCounter atomic.Int64

func nextRequestID() string {
	return fmt.Sprintf("bench-%d", requestCounter.Add(1))
}

func manyChannelNames(e *env, count int) []string {
	names := make([]string, count)
	prefix := e.channel("many")
	for i := range names {
		names[i] = fmt.Sprintf("%s.%d", prefix, i)
	}
	return names
}

func deleteAll(e *env, names []string) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(names) {
					return
				}
				e.deleteChannel("queues", names[i])
			}
		}()
	}
	wg.Wait()
}

// fillChannels sends one message to each channel, which creates them.
func fillChannels(e *env, names []string, recorders []latencies, body []byte, clientID string) (int64, error) {
	var next, failed atomic.Int64
	var firstErr atomic.Value
	var wg sync.WaitGroup
	for w := range recorders {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(names) {
					return
				}
				t := time.Now()
				if err := checkSend(e.client.SendQueueMessage(e.ctx, queueMessage(names[i], clientID, body))); err != nil {
					failed.Add(1)
					firstErr.CompareAndSwap(nil, err)
					continue
				}
				recorders[w].add(time.Since(t))
			}
		}(w)
	}
	wg.Wait()
	err, _ := firstErr.Load().(error)
	return failed.Load(), err
}

// manyChannelsCreate shows what creating a channel costs, since each one is a
// stream with files of its own.
func manyChannelsCreate(e *env, count int) (Sample, error) {
	names := manyChannelNames(e, count)
	e.later(func() { deleteAll(e, names) })
	recorders := make([]latencies, 8)
	body := make([]byte, kb)
	start := time.Now()
	failed, err := fillChannels(e, names, recorders, body, e.clientID("create"))
	sample := finish(start, int64(count)-failed, kb, recorders)
	sample.Failed = failed
	return sample, err
}

func manyChannelsSend(e *env, size, workers, count int) (Sample, error) {
	names := manyChannelNames(e, count)
	e.later(func() { deleteAll(e, names) })
	body := make([]byte, size)
	clientID := e.clientID("send")
	if _, err := fillChannels(e, names, make([]latencies, 8), body, clientID); err != nil {
		return Sample{}, fmt.Errorf("seed: %w", err)
	}
	var next atomic.Int64
	e.startMeasuring()
	return e.closedLoop(size, workers, 1, func(ctx context.Context, _ int) error {
		name := names[int(next.Add(1))%len(names)]
		return checkSend(e.client.SendQueueMessage(ctx, queueMessage(name, clientID, body)))
	}), nil
}
