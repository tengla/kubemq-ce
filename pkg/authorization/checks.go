package authorization

import (
	"github.com/kubemq-io/kubemq-community/pkg/entities"
	pb "github.com/kubemq-io/protobuf/go"
)

// ChecksFor lists what a client message does, for the message types of the
// gRPC service, which the REST API shares. Messages that act on something
// the client already received, such as acks and responses, need no checks.
//
// A list-channels query also needs none here: the server lists only the
// channels the caller may manage.
func ChecksFor(msg any) []Check {
	switch m := msg.(type) {
	case *pb.Event:
		if m.Store {
			return []Check{{Send, "events_store", m.Channel}}
		}
		return []Check{{Send, "events", m.Channel}}
	case *pb.Subscribe:
		switch m.SubscribeTypeData {
		case pb.Subscribe_Events:
			return []Check{{Receive, "events", m.Channel}}
		case pb.Subscribe_EventsStore:
			return []Check{{Receive, "events_store", m.Channel}}
		case pb.Subscribe_Commands:
			return []Check{{Receive, "commands", m.Channel}}
		case pb.Subscribe_Queries:
			return []Check{{Receive, "queries", m.Channel}}
		}
	case *pb.Request:
		if m.Channel == entities.ClusterInternalRequestsChannel {
			if m.Metadata == "list-channels" {
				return nil
			}
			return []Check{{Manage, m.Tags["channel_type"], m.Tags["channel"]}}
		}
		switch m.RequestTypeData {
		case pb.Request_Command:
			return []Check{{Send, "commands", m.Channel}}
		case pb.Request_Query:
			return []Check{{Send, "queries", m.Channel}}
		}
	case *pb.QueueMessage:
		return queueMessagesChecks([]*pb.QueueMessage{m})
	case *pb.QueueMessagesBatchRequest:
		return queueMessagesChecks(m.Messages)
	case *pb.QueuesUpstreamRequest:
		return queueMessagesChecks(m.Messages)
	case *pb.ReceiveQueueMessagesRequest:
		return []Check{{Receive, "queues", m.Channel}}
	case *pb.AckAllQueueMessagesRequest:
		return []Check{{Receive, "queues", m.Channel}}
	case *pb.StreamQueueMessagesRequest:
		switch m.StreamRequestTypeData {
		case pb.StreamRequestType_ReceiveMessage:
			return []Check{{Receive, "queues", m.Channel}}
		case pb.StreamRequestType_ResendMessage:
			return []Check{{Send, "queues", m.Channel}}
		case pb.StreamRequestType_SendModifiedMessage:
			if m.ModifiedMessage != nil {
				return []Check{{Send, "queues", m.ModifiedMessage.Channel}}
			}
		}
	case *pb.QueuesDownstreamRequest:
		switch m.RequestTypeData {
		case pb.QueuesDownstreamRequestType_Get:
			return []Check{{Receive, "queues", m.Channel}}
		case pb.QueuesDownstreamRequestType_ReQueueAll, pb.QueuesDownstreamRequestType_ReQueueRange:
			return []Check{{Send, "queues", m.ReQueueChannel}}
		}
	case *pb.QueuesInfoRequest:
		return []Check{{Manage, "queues", m.QueueName}}
	}
	return nil
}

// queueMessagesChecks checks each queue a batch sends to once. A message's
// dead-letter queue (MaxReceiveQueue) counts as a send too, since the server
// moves the message there.
func queueMessagesChecks(msgs []*pb.QueueMessage) []Check {
	var checks []Check
	seen := map[string]bool{}
	add := func(channel string) {
		if !seen[channel] {
			seen[channel] = true
			checks = append(checks, Check{Send, "queues", channel})
		}
	}
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		add(msg.Channel)
		if msg.Policy != nil && msg.Policy.MaxReceiveQueue != "" {
			add(msg.Policy.MaxReceiveQueue)
		}
	}
	return checks
}
