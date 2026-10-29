package array

import (
	"errors"

	"github.com/kubemq-io/kubemq-community/pkg/tracing"
	pb "github.com/kubemq-io/protobuf/go"
)

func eventTraceMessage(msg *pb.Event) tracing.Message {
	pattern := "events"
	if msg.Store {
		pattern = "events_store"
	}
	return tracing.Message{Pattern: pattern, Channel: msg.Channel, ID: msg.EventID, ClientID: msg.ClientID, BodySize: len(msg.Body)}
}

func requestTraceMessage(pattern string, req *pb.Request) tracing.Message {
	return tracing.Message{Pattern: pattern, Channel: req.Channel, ID: req.RequestID, ClientID: req.ClientID, BodySize: len(req.Body)}
}

func queueTraceMessage(msg *pb.QueueMessage) tracing.Message {
	return tracing.Message{Pattern: "queues", Channel: msg.Channel, ID: msg.MessageID, ClientID: msg.ClientID, BodySize: len(msg.Body)}
}

func eventError(result *pb.Result, err error) error {
	if err != nil {
		return err
	}
	if result != nil && !result.Sent {
		return errors.New(result.Error)
	}
	return nil
}

func queueResultError(result *pb.SendQueueMessageResult) error {
	if result != nil && result.IsError {
		return errors.New(result.Error)
	}
	return nil
}
