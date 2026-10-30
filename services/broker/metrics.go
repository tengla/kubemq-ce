package broker

import (
	"strings"
	"time"

	"github.com/kubemq-io/kubemq-community/pkg/jstore"
	natsd "github.com/nats-io/nats-server/v2/server"
)

func toQueues(details []natsd.StreamDetail, prefix string) *Queues {
	queues := &Queues{Now: time.Now()}
	for _, detail := range details {
		if detail.Config == nil || len(detail.Config.Subjects) == 0 || !strings.HasPrefix(detail.Config.Subjects[0], prefix) {
			continue
		}
		q := &Queue{
			Name:          strings.TrimPrefix(detail.Config.Subjects[0], prefix),
			Messages:      int64(detail.State.Msgs),
			Bytes:         int64(detail.State.Bytes),
			FirstSequence: int64(detail.State.FirstSeq),
			LastSequence:  int64(detail.State.LastSeq),
		}
		for _, consumer := range detail.Consumer {
			if consumer == nil {
				continue
			}
			q.clients = append(q.clients, &Client{
				ClientId:         consumer.Name,
				Active:           jstore.ConsumerActive(consumer.NumWaiting, consumer.NumAckPending),
				LastSequenceSent: int64(consumer.Delivered.Stream),
				Pending:          int64(consumer.NumAckPending),
			})
		}
		if strings.HasPrefix(prefix, jstore.QueueSubjectPrefix) {
			q.calcWorkQueue()
		} else {
			q.Calc()
		}
		queues.Sent += q.Sent
		queues.Delivered += q.Delivered
		queues.Waiting += q.Waiting
		queues.Queues = append(queues.Queues, q)
	}
	queues.TotalQueues = len(queues.Queues)
	return queues
}

type Queue struct {
	Name          string `json:"name"`
	Messages      int64  `json:"messages"`
	Bytes         int64  `json:"bytes"`
	FirstSequence int64  `json:"first_sequence"`
	LastSequence  int64  `json:"last_sequence"`
	Sent          int64  `json:"sent"`
	Subscribers   int    `json:"subscribers"`
	Waiting       int64  `json:"waiting"`
	Delivered     int64  `json:"delivered"`
	clients       []*Client
}

// calcWorkQueue works out the counts of a queue, whose stream keeps only the
// messages nobody has acked.
func (q *Queue) calcWorkQueue() {
	q.Subscribers = len(q.clients)
	q.Sent = q.LastSequence
	q.Waiting = q.Messages
	for _, c := range q.clients {
		q.Waiting -= c.Pending
	}
	if q.Waiting < 0 {
		q.Waiting = 0
	}
	q.Delivered = q.Sent - q.Waiting
}

func (q *Queue) Calc() {
	if q.LastSequence == 0 || q.FirstSequence > q.LastSequence {
		return
	}
	q.Sent = q.LastSequence - q.FirstSequence + 1
	if len(q.clients) == 0 {
		q.Waiting = q.LastSequence - q.FirstSequence + 1
		return
	}
	q.Subscribers = len(q.clients)
	maxSequenceSent := int64(0)
	pendingCount := int64(0)

	for _, c := range q.clients {
		if c.LastSequenceSent <= q.LastSequence {
			pendingCount += c.Pending
		}
		if c.LastSequenceSent > maxSequenceSent {
			maxSequenceSent = c.LastSequenceSent
		}
		if c.LastSequenceSent >= q.FirstSequence {
			q.Delivered += c.LastSequenceSent - q.FirstSequence + 1
		}
	}
	q.Waiting = pendingCount + q.LastSequence - maxSequenceSent
}

type Client struct {
	ClientId         string `json:"client_id"`
	Active           bool   `json:"active"`
	LastSequenceSent int64  `json:"last_sequence_sent"`
	IsStalled        bool   `json:"is_stalled"`
	Pending          int64  `json:"pending"`
}

type Queues struct {
	Now         time.Time `json:"now"`
	TotalQueues int       `json:"total_queues"`
	Sent        int64     `json:"sent"`
	Waiting     int64     `json:"waiting"`
	Delivered   int64     `json:"delivered"`
	Queues      []*Queue  `json:"queues"`
}

func (q *Queues) Filter(exp string) *Queues {
	if exp == "" {
		return q
	}
	results := &Queues{
		Now:         time.Now(),
		TotalQueues: 0,
		Sent:        0,
		Waiting:     0,
		Delivered:   0,
		Queues:      nil,
	}
	for _, queue := range q.Queues {
		for _, param := range strings.Split(exp, ";") {
			if queue.Name == param {
				results.Queues = append(results.Queues, queue)
				continue
			}
		}
	}
	for _, queue := range results.Queues {
		results.TotalQueues++
		results.Sent += queue.Sent
		results.Waiting += queue.Waiting
		results.Delivered += queue.Delivered
	}
	return results
}
