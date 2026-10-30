package client

import (
	"github.com/kubemq-io/kubemq-community/pkg/jstore"
)

type DispatchItem struct {
	id      int
	channel string
	data    []byte
}
type AsyncDispatcher struct {
	conn *jstore.Conn
}

func NewDispatcher(conn *jstore.Conn) *AsyncDispatcher {
	return &AsyncDispatcher{conn: conn}
}

// DispatchAsync sends the items in order and returns the outcome of each by id.
func (ad *AsyncDispatcher) DispatchAsync(items []*DispatchItem) map[int]error {
	batch := make([]jstore.Item, len(items))
	for i, item := range items {
		batch[i] = jstore.Item{Subject: item.channel, Data: item.data}
	}
	result := make(map[int]error, len(items))
	for i, err := range ad.conn.PublishBatch(batch) {
		result[items[i].id] = err
	}
	return result
}
