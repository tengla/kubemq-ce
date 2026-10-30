// Package jstore keeps the messages of queues and events store in JetStream.
//
// Each channel is one stream with one subject, so sequences count per channel,
// as clients expect, and a channel can be limited and deleted on its own.
// Subscribers pull messages through a consumer, holding at most MaxInflight
// unacknowledged messages. What a subscriber still holds when it closes goes
// back to the channel right away, in order, for the next subscriber.
//
// Queue channels use work-queue retention: acking removes the message from the
// stream, and that write survives a crash better than a consumer's state, which
// the server saves periodically. So a queue's consumer holds nothing worth
// keeping, and ResetQueueConsumers starts them afresh when the broker starts.
package jstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kubemq-io/kubemq-community/pkg/pipe"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	publishTimeout  = 60 * time.Second
	apiTimeout      = 10 * time.Second
	asyncMaxPending = 4096

	// QueueSubjectPrefix starts the subjects of queue channels, including the
	// one that holds delayed messages.
	QueueSubjectPrefix = "_QUEUE"

	errCodeMessageTooLarge = 10077
)

var (
	ErrClientIDInUse   = errors.New("client id is already connected")
	ErrInvalidClient   = errors.New("invalid client id: only alphanumeric, '-' and '_' characters are allowed")
	ErrMessageTooLarge = errors.New("message too large: a stored message can be at most about 32 MiB")

	clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// StreamName returns the stream that holds a subject. Subjects have characters
// stream names can't have, and stream names become directory names, so it's a
// hash; the subject itself is in the stream's configuration.
func StreamName(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "KUBEMQ_" + hex.EncodeToString(sum[:16])
}

// Conn publishes and subscribes on one NATS connection.
type Conn struct {
	nc       *nats.Conn
	js       jetstream.JetStream
	pipe     *pipe.Pipe
	clientID string

	mu      sync.Mutex
	streams map[string]struct{}
	subs    map[*Subscription]struct{}
	// consumers are the stream/consumer pairs known to exist.
	consumers map[string]struct{}
	closed    bool
}

// Connect reserves the client ID and returns a Conn that uses nc. The caller
// still owns nc and closes it after the Conn.
func Connect(nc *nats.Conn, p *pipe.Pipe, clientID string) (*Conn, error) {
	if !clientIDPattern.MatchString(clientID) {
		return nil, ErrInvalidClient
	}
	js, err := jetstream.New(nc,
		jetstream.WithPublishAsyncMaxPending(asyncMaxPending),
		jetstream.WithPublishAsyncTimeout(publishTimeout),
	)
	if err != nil {
		return nil, err
	}
	if !p.RegisterClient(clientID) {
		return nil, ErrClientIDInUse
	}
	c := &Conn{
		nc:       nc,
		js:       js,
		pipe:     p,
		clientID: clientID,
		streams:  make(map[string]struct{}),
		subs:     make(map[*Subscription]struct{}),

		consumers: make(map[string]struct{}),
	}
	// A connection can end without anyone closing this Conn, as when the broker
	// shuts down first.
	closed := nc.Opts.ClosedCB
	nc.SetClosedHandler(func(conn *nats.Conn) {
		_ = c.Close()
		if closed != nil {
			closed(conn)
		}
	})
	return c, nil
}

// Close closes the Conn's subscriptions, handing back what they hold.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	subs := make([]*Subscription, 0, len(c.subs))
	for sub := range c.subs {
		subs = append(subs, sub)
	}
	c.mu.Unlock()
	for _, sub := range subs {
		_ = sub.Close()
	}
	c.pipe.ReleaseClient(c.clientID)
	return nil
}

func (c *Conn) ensureStream(ctx context.Context, subject string) (string, error) {
	name := StreamName(subject)
	c.mu.Lock()
	_, known := c.streams[name]
	c.mu.Unlock()
	if known {
		return name, nil
	}
	limits := c.pipe.Limits()
	retention := jetstream.LimitsPolicy
	if strings.HasPrefix(subject, QueueSubjectPrefix) {
		retention = jetstream.WorkQueuePolicy
	}
	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  []string{subject},
		Storage:   jetstream.FileStorage,
		Retention: retention,
		Discard:   jetstream.DiscardOld,
		Replicas:  1,
		MaxAge:    time.Duration(limits.MaxAgeMinutes) * time.Minute,
		MaxBytes:  orUnlimited(limits.MaxBytes),
		MaxMsgs:   orUnlimited(limits.MaxMsgs),
	})
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.streams[name] = struct{}{}
	c.mu.Unlock()
	return name, nil
}

func orUnlimited(v int64) int64 {
	if v <= 0 {
		return -1
	}
	return v
}

func (c *Conn) forgetStream(name string) {
	c.mu.Lock()
	delete(c.streams, name)
	for key := range c.consumers {
		if strings.HasPrefix(key, name+"/") {
			delete(c.consumers, key)
		}
	}
	c.mu.Unlock()
}

func (c *Conn) forgetConsumer(key string) {
	c.mu.Lock()
	delete(c.consumers, key)
	c.mu.Unlock()
}

func publishError(err error) error {
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode == errCodeMessageTooLarge {
		return ErrMessageTooLarge
	}
	return err
}

// ResetQueueConsumers deletes the consumers of queue channels, so unacknowledged
// messages are delivered from the start and in order, instead of when their
// holds run out. Call it when the broker starts, before any client connects.
func ResetQueueConsumers(ctx context.Context, js jetstream.JetStream) error {
	streams := js.ListStreams(ctx)
	for info := range streams.Info() {
		if len(info.Config.Subjects) == 0 || !strings.HasPrefix(info.Config.Subjects[0], QueueSubjectPrefix) {
			continue
		}
		err := js.DeleteConsumer(ctx, info.Config.Name, queueConsumer)
		if err != nil && !errors.Is(err, jetstream.ErrConsumerNotFound) {
			return wrapf(err, "reset consumer on %s", info.Config.Subjects[0])
		}
	}
	return streams.Err()
}

// Publish stores a message and returns once it's stored.
func (c *Conn) Publish(subject string, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	name, err := c.ensureStream(ctx, subject)
	if err != nil {
		return err
	}
	_, err = c.js.Publish(ctx, subject, data)
	if errors.Is(err, jetstream.ErrNoStreamResponse) {
		// Someone deleted the channel after this Conn last used it.
		c.forgetStream(name)
		if _, err = c.ensureStream(ctx, subject); err != nil {
			return err
		}
		_, err = c.js.Publish(ctx, subject, data)
	}
	return publishError(err)
}

// Item is one message of a PublishBatch.
type Item struct {
	Subject string
	Data    []byte
}

// PublishBatch stores messages in order and returns once each is stored or
// failed. The result has one error per item.
func (c *Conn) PublishBatch(items []Item) []error {
	results := make([]error, len(items))
	futures := make([]jetstream.PubAckFuture, len(items))
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	for i, item := range items {
		if _, err := c.ensureStream(ctx, item.Subject); err != nil {
			results[i] = err
			continue
		}
		futures[i], results[i] = c.js.PublishAsync(item.Subject, item.Data, jetstream.WithStallWait(publishTimeout))
	}
	for i, future := range futures {
		if future == nil {
			continue
		}
		select {
		case <-future.Ok():
		case err := <-future.Err():
			if errors.Is(err, jetstream.ErrNoStreamResponse) {
				c.forgetStream(StreamName(items[i].Subject))
				err = c.Publish(items[i].Subject, items[i].Data)
			}
			results[i] = publishError(err)
		}
	}
	return results
}

// ConsumerActive reports whether a consumer has a live subscriber. A subscriber
// always has a pull request waiting or messages in hand.
func ConsumerActive(waiting, ackPending int) bool {
	return waiting > 0 || ackPending > 0
}

func wrapf(err error, format string, args ...interface{}) error {
	return fmt.Errorf(format+": %w", append(args, err)...)
}
