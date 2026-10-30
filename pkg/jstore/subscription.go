package jstore

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	pullExpires     = 30 * time.Second
	maxWaiting      = 4096
	drainTimeout    = 2 * time.Second
	drainPoll       = 50 * time.Microsecond
	recoverInterval = 200 * time.Millisecond

	defaultMaxInflight = 1024
	queueConsumer      = "queue"
)

// A held message goes back to the channel after ackWait unless renewed, which
// limits how long a crash can hide it. Variables for the tests.
var (
	ackWait       = 30 * time.Second
	renewInterval = 10 * time.Second
)

// ErrSubscriptionClosed is returned when acking a message of a closed
// subscription. The message went back to the channel when it closed.
var ErrSubscriptionClosed = errors.New("subscription is closed")

// Msg is a message delivered to a subscriber.
type Msg struct {
	Data      []byte
	Timestamp int64
	Sequence  uint64

	sub *Subscription
	raw *nats.Msg
}

// Ack removes the message from what the subscriber holds, for good.
func (m *Msg) Ack() error {
	return m.sub.ack(m)
}

// Release hands the message back to the channel, for the next subscriber.
func (m *Msg) Release() error {
	return m.sub.release(m)
}

type subOptions struct {
	durable     string
	deliver     jetstream.DeliverPolicy
	startSeq    uint64
	startTime   *time.Time
	maxInflight int
	manualAck   bool
	expiring    bool
}

// SubOption configures a subscription.
type SubOption func(*subOptions)

// QueueDurable subscribes through the channel's one shared queue consumer.
func QueueDurable() SubOption {
	return func(o *subOptions) { o.durable = queueConsumer }
}

// Durable subscribes through a named consumer whose position outlives the
// subscription. It expires after the configured time without a subscriber.
func Durable(name string) SubOption {
	return func(o *subOptions) {
		o.durable = "sub_" + StreamName(name)
		o.expiring = true
	}
}

// DeliverAll starts a new consumer at the channel's first message.
func DeliverAll() SubOption {
	return func(o *subOptions) { o.deliver = jetstream.DeliverAllPolicy }
}

// DeliverNew starts a new consumer with the next message sent.
func DeliverNew() SubOption {
	return func(o *subOptions) { o.deliver = jetstream.DeliverNewPolicy }
}

// DeliverLast starts a new consumer at the channel's last message.
func DeliverLast() SubOption {
	return func(o *subOptions) { o.deliver = jetstream.DeliverLastPolicy }
}

// StartAtSequence starts a new consumer at a sequence.
func StartAtSequence(seq uint64) SubOption {
	return func(o *subOptions) {
		o.deliver = jetstream.DeliverByStartSequencePolicy
		o.startSeq = seq
	}
}

// StartAtTime starts a new consumer at a time.
func StartAtTime(t time.Time) SubOption {
	return func(o *subOptions) {
		o.deliver = jetstream.DeliverByStartTimePolicy
		o.startTime = &t
	}
}

// MaxInflight sets how many unacknowledged messages the subscriber can hold.
func MaxInflight(n int) SubOption {
	return func(o *subOptions) { o.maxInflight = n }
}

// ManualAck leaves acking to the subscriber instead of acking when the callback
// returns.
func ManualAck() SubOption {
	return func(o *subOptions) { o.manualAck = true }
}

// Subscription delivers a channel's messages to a callback, one at a time.
type Subscription struct {
	conn     *Conn
	subject  string
	stream   string
	consumer string
	opts     subOptions
	cb       func(*Msg)
	inbox    *nats.Subscription

	mu          sync.Mutex
	held        map[uint64]*Msg
	queue       []*Msg
	outstanding int
	closed      bool
	recovering  bool
	stopped     bool
	wake        chan struct{}
	done        chan struct{}

	// dispatching is held while a message goes through the callback.
	dispatching sync.Mutex
}

// Subscribe starts delivering a channel's messages. It creates the channel and
// the consumer if they don't exist; an existing consumer keeps its position.
func (c *Conn) Subscribe(subject string, cb func(*Msg), options ...SubOption) (*Subscription, error) {
	opts := subOptions{maxInflight: defaultMaxInflight}
	for _, option := range options {
		option(&opts)
	}
	s := &Subscription{
		conn:    c,
		subject: subject,
		stream:  StreamName(subject),
		opts:    opts,
		cb:      cb,
		held:    make(map[uint64]*Msg),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	if err := s.ensureConsumer(ctx); err != nil {
		return nil, err
	}
	var err error
	s.inbox, err = c.nc.Subscribe(nats.NewInbox(), s.onMsg)
	if err != nil {
		return nil, err
	}
	// Bursts of a large window must not count as a slow consumer.
	_ = s.inbox.SetPendingLimits(-1, -1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = s.inbox.Unsubscribe()
		return nil, ErrSubscriptionClosed
	}
	c.subs[s] = struct{}{}
	c.mu.Unlock()

	go s.dispatch()
	go s.renew(renewInterval)
	s.mu.Lock()
	s.pump()
	s.mu.Unlock()
	return s, nil
}

func (s *Subscription) ensureConsumer(ctx context.Context) error {
	if _, err := s.conn.ensureStream(ctx, s.subject); err != nil {
		return wrapf(err, "create channel %s", s.subject)
	}
	cfg := jetstream.ConsumerConfig{
		Durable:       s.opts.durable,
		DeliverPolicy: s.opts.deliver,
		OptStartSeq:   s.opts.startSeq,
		OptStartTime:  s.opts.startTime,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       ackWait,
		MaxDeliver:    -1,
		MaxAckPending: -1,
		MaxWaiting:    maxWaiting,
		Replicas:      1,
	}
	if s.opts.durable == "" {
		cfg.MemoryStorage = true
		cfg.InactiveThreshold = time.Minute
	}
	if s.opts.expiring {
		cfg.InactiveThreshold = time.Duration(s.conn.pipe.Limits().ConsumerInactiveMinutes) * time.Minute
	}
	if s.opts.durable != "" {
		key := s.stream + "/" + s.opts.durable
		s.conn.mu.Lock()
		_, known := s.conn.consumers[key]
		s.conn.mu.Unlock()
		if known {
			s.consumer = s.opts.durable
			return nil
		}
		// An existing consumer keeps its position, whatever this subscriber
		// asks for.
		consumer, err := s.conn.js.Consumer(ctx, s.stream, s.opts.durable)
		if err == nil {
			s.consumer = consumer.CachedInfo().Name
			s.rememberConsumer(key)
			return nil
		}
		if !errors.Is(err, jetstream.ErrConsumerNotFound) {
			return wrapf(err, "look up consumer on %s", s.subject)
		}
	}
	consumer, err := s.conn.js.CreateConsumer(ctx, s.stream, cfg)
	if err != nil {
		if errors.Is(err, jetstream.ErrConsumerExists) && s.opts.durable != "" {
			// Another subscriber created it just now, with other options.
			s.consumer = s.opts.durable
			return nil
		}
		return wrapf(err, "create consumer on %s", s.subject)
	}
	s.consumer = consumer.CachedInfo().Name
	if s.opts.durable != "" {
		s.rememberConsumer(s.stream + "/" + s.opts.durable)
	}
	return nil
}

func (s *Subscription) rememberConsumer(key string) {
	s.conn.mu.Lock()
	s.conn.consumers[key] = struct{}{}
	s.conn.mu.Unlock()
}

// pump asks for as many messages as the window has room for. There's one
// request at a time; it ends full or with a status message. Call with s.mu held.
func (s *Subscription) pump() {
	if s.closed || s.stopped || s.recovering || s.outstanding > 0 {
		return
	}
	free := s.opts.maxInflight - len(s.held)
	if free <= 0 {
		return
	}
	body, err := json.Marshal(struct {
		Batch   int   `json:"batch"`
		Expires int64 `json:"expires"`
	}{free, pullExpires.Nanoseconds()})
	if err != nil {
		return
	}
	subject := "$JS.API.CONSUMER.MSG.NEXT." + s.stream + "." + s.consumer
	if err := s.conn.nc.PublishRequest(subject, s.inbox.Subject, body); err != nil {
		return
	}
	s.outstanding = free
}

func (s *Subscription) onMsg(raw *nats.Msg) {
	if status := raw.Header.Get("Status"); status != "" && len(raw.Data) == 0 {
		s.onStatus(status)
		return
	}
	meta, err := raw.Metadata()
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed && s.held == nil {
		// It took too long to arrive; Close has finished handing back.
		_ = raw.Nak()
		return
	}
	s.outstanding--
	if current, ok := s.held[meta.Sequence.Stream]; ok {
		// Redelivered after a stall in renewing; the subscriber has it already.
		current.raw = raw
		return
	}
	msg := &Msg{
		Data:      raw.Data,
		Timestamp: meta.Timestamp.UnixNano(),
		Sequence:  meta.Sequence.Stream,
		sub:       s,
		raw:       raw,
	}
	s.held[msg.Sequence] = msg
	if s.closed {
		// On its way when the subscription closed. Close hands it back.
		return
	}
	s.queue = append(s.queue, msg)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	s.pump()
}

func (s *Subscription) onStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outstanding = 0
	if s.closed {
		return
	}
	switch status {
	case "404", "408":
		s.pump()
	default:
		// 503 means the channel is gone, 409 that the consumer changed or is
		// gone. Create them again, and don't spin if that fails.
		if !s.recovering {
			s.recovering = true
			go s.recover()
		}
	}
}

func (s *Subscription) recover() {
	s.conn.forgetStream(s.stream)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		err := s.ensureConsumer(ctx)
		cancel()
		if err == nil {
			s.mu.Lock()
			s.recovering = false
			s.pump()
			s.mu.Unlock()
			return
		}
		select {
		case <-s.done:
			return
		case <-time.After(recoverInterval):
		}
	}
}

// dispatch runs the callback for one message at a time. A callback can block,
// so this is apart from onMsg, which has to keep the held messages up to date.
func (s *Subscription) dispatch() {
	for {
		s.dispatching.Lock()
		s.mu.Lock()
		var msg *Msg
		if len(s.queue) > 0 {
			msg = s.queue[0]
			s.queue[0] = nil
			s.queue = s.queue[1:]
		}
		s.mu.Unlock()
		if msg == nil {
			s.dispatching.Unlock()
			select {
			case <-s.wake:
				continue
			case <-s.done:
				return
			}
		}
		s.cb(msg)
		if !s.opts.manualAck {
			_ = msg.Ack()
		}
		s.dispatching.Unlock()
		select {
		case <-s.done:
			return
		default:
		}
	}
}

// renew keeps the server from redelivering what the subscriber still holds.
func (s *Subscription) renew(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			for _, msg := range s.held {
				_ = msg.raw.InProgress()
			}
			s.mu.Unlock()
		case <-s.done:
			return
		}
	}
}

func (s *Subscription) ack(msg *Msg) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSubscriptionClosed
	}
	current, ok := s.held[msg.Sequence]
	if !ok {
		return nil
	}
	if err := current.raw.Ack(); err != nil {
		return err
	}
	delete(s.held, msg.Sequence)
	s.pump()
	return nil
}

func (s *Subscription) release(msg *Msg) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSubscriptionClosed
	}
	current, ok := s.held[msg.Sequence]
	if !ok {
		return nil
	}
	delete(s.held, msg.Sequence)
	return current.raw.Nak()
}

// StopFetching ends asking for more messages. Call it before acking the last
// messages a subscriber wants, so acking doesn't bring in others it won't use.
func (s *Subscription) StopFetching() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// HandBack ends delivery but keeps what the callback took, for a subscriber that
// has finished receiving and may ack much later. Its waiting request is canceled
// and what hasn't reached the callback goes back to the channel, in order, so
// other subscribers can get it meanwhile. The callback must not block, and must
// Release what it doesn't take from then on.
func (s *Subscription) HandBack() {
	s.StopFetching()
	s.dispatching.Lock()
	defer s.dispatching.Unlock()
	// First, or the request would get back what's handed back here.
	s.drainInbox()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	queued := s.queue
	s.queue = nil
	for _, msg := range queued {
		delete(s.held, msg.Sequence)
	}
	s.mu.Unlock()
	s.giveBack(queued)
}

// giveBack hands messages back, lowest sequence first. Waiting for the last one
// means the server has them all, so the next subscriber starts with these. Call
// it after draining the inbox.
func (s *Subscription) giveBack(list []*Msg) {
	if len(list) == 0 {
		return
	}
	// The server forgets a canceled request when it next looks at it. If it found
	// out while delivering, the first message here would go behind the others, and
	// asking about the consumer makes it look now.
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	_, _ = s.conn.js.Consumer(ctx, s.stream, s.consumer)
	cancel()

	sort.Slice(list, func(i, j int) bool { return list[i].Sequence < list[j].Sequence })
	for i, msg := range list {
		if i < len(list)-1 {
			_ = msg.raw.Nak()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		_ = msg.raw.Nak(nats.Context(ctx))
		cancel()
	}
}

// drainInbox removes the waiting request at the server and still runs onMsg for
// messages already on their way. A request that got all its messages is over at
// the server, so then there's nothing to wait for.
func (s *Subscription) drainInbox() {
	s.mu.Lock()
	waiting := s.outstanding > 0
	s.mu.Unlock()
	if !waiting {
		_ = s.inbox.Unsubscribe()
		return
	}
	if err := s.inbox.Drain(); err == nil {
		deadline := time.Now().Add(drainTimeout)
		for s.inbox.IsDraining() && time.Now().Before(deadline) {
			time.Sleep(drainPoll)
		}
	}
}

// Close ends the subscription and hands back what it holds, lowest sequence
// first. A durable consumer keeps its position.
func (s *Subscription) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.queue = nil
	s.mu.Unlock()
	close(s.done)

	s.conn.mu.Lock()
	delete(s.conn.subs, s)
	s.conn.mu.Unlock()

	// First, or the waiting request would get back what's handed back here. What
	// arrives meanwhile joins the held messages.
	s.drainInbox()
	s.mu.Lock()
	held := make([]*Msg, 0, len(s.held))
	for _, msg := range s.held {
		held = append(held, msg)
	}
	s.held = nil
	s.mu.Unlock()
	s.giveBack(held)

	if s.opts.durable == "" && s.consumer != "" {
		s.deleteConsumer()
	}
	return nil
}

// Unsubscribe closes the subscription and forgets a durable position.
func (s *Subscription) Unsubscribe() error {
	if err := s.Close(); err != nil {
		return err
	}
	if s.opts.expiring {
		s.deleteConsumer()
	}
	return nil
}

func (s *Subscription) deleteConsumer() {
	s.conn.forgetConsumer(s.stream + "/" + s.consumer)
	ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
	defer cancel()
	_ = s.conn.js.DeleteConsumer(ctx, s.stream, s.consumer)
}
