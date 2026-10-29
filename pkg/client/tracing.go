package client

import (
	"github.com/kubemq-io/kubemq-community/pkg/tracing"
	pb "github.com/kubemq-io/protobuf/go"
)

// The delivery spans name the subscriber, not the sender: the send span
// already has the sender.

func eventDeliveryTrace(pattern string, msg *pb.EventReceive, subReq *pb.Subscribe) tracing.Message {
	return tracing.Message{Pattern: pattern, Channel: msg.Channel, ID: msg.EventID, ClientID: subReq.ClientID, Group: subReq.Group, BodySize: len(msg.Body)}
}

func requestDeliveryTrace(pattern string, req *pb.Request, subReq *pb.Subscribe) tracing.Message {
	return tracing.Message{Pattern: pattern, Channel: req.Channel, ID: req.RequestID, ClientID: subReq.ClientID, Group: subReq.Group, BodySize: len(req.Body)}
}
