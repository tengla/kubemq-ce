package broker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueue_CalcWorkQueue(t *testing.T) {
	tests := []struct {
		name      string
		messages  int64
		last      int64
		pending   []int64
		waiting   int64
		delivered int64
	}{
		{name: "nobody received", messages: 6, last: 6, waiting: 6},
		{name: "held or handed back", messages: 6, last: 6, pending: []int64{6}, waiting: 6, delivered: 6},
		{name: "some held", messages: 4, last: 6, pending: []int64{1}, waiting: 4, delivered: 3},
		{name: "all acked", last: 6, pending: []int64{0}, delivered: 6},
		{name: "never above sent", messages: 2, last: 2, pending: []int64{2, 2}, waiting: 2, delivered: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &Queue{Messages: tt.messages, LastSequence: tt.last}
			for _, pending := range tt.pending {
				q.clients = append(q.clients, &Client{Pending: pending})
			}
			q.calcWorkQueue()
			require.Equal(t, tt.last, q.Sent)
			require.Equal(t, tt.waiting, q.Waiting)
			require.Equal(t, tt.delivered, q.Delivered)
		})
	}
}
