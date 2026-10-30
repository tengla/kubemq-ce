package client

import (
	"github.com/kubemq-io/kubemq-community/pkg/entities"
	"github.com/kubemq-io/kubemq-community/pkg/jstore"
	pb "github.com/kubemq-io/protobuf/go"
	"time"
)

func UnmarshalToEventReceive(data []byte) (*pb.EventReceive, error) {
	msg := &pb.Event{}
	err := msg.Unmarshal(data)
	if err != nil {
		return nil, err
	}

	qm := &pb.EventReceive{
		EventID:              msg.EventID,
		Channel:              msg.Channel,
		Metadata:             msg.Metadata,
		Body:                 msg.Body,
		Timestamp:            0,
		Sequence:             0,
		Tags:                 msg.Tags,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	return qm, err
}

// ParseSubscriptionRequest returns the options of an events store subscription.
// A group shares one position. Without a group, the position is the client's,
// so a client that comes back with the same ID continues where it left off.
func ParseSubscriptionRequest(req *pb.Subscribe) []jstore.SubOption {
	durable := "group|" + req.Group
	if req.Group == "" {
		durable = "client|" + req.ClientID
	}
	var list = []jstore.SubOption{jstore.Durable(durable)}
	switch req.EventsStoreTypeData {

	case pb.Subscribe_StartFromLast:
		list = append(list, jstore.DeliverLast())
	case pb.Subscribe_StartNewOnly:
		list = append(list, jstore.DeliverNew())
	case pb.Subscribe_StartFromFirst:
		list = append(list, jstore.DeliverAll())
	case pb.Subscribe_StartAtSequence:
		list = append(list, jstore.StartAtSequence(uint64(req.EventsStoreTypeValue)))
	case pb.Subscribe_StartAtTime:
		list = append(list, jstore.StartAtTime(time.Unix(0, req.EventsStoreTypeValue)))
	case pb.Subscribe_StartAtTimeDelta:
		list = append(list, jstore.StartAtTime(time.Now().Add(-time.Duration(req.EventsStoreTypeValue)*time.Second)))

	}
	return list
}

func validateOptions(opts *Options, forQueue bool) error {
	if opts.ClientID == "" {
		return entities.ErrInvalidClientID
	}
	return nil
}

func unmarshalToEventReceive(data []byte) (*pb.EventReceive, error) {
	msg := &pb.Event{}
	err := msg.Unmarshal(data)
	if err != nil {
		return nil, err
	}

	qm := &pb.EventReceive{
		EventID:              msg.EventID,
		Channel:              msg.Channel,
		Metadata:             msg.Metadata,
		Body:                 msg.Body,
		Timestamp:            0,
		Sequence:             0,
		Tags:                 msg.Tags,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_sizecache:        0,
	}

	return qm, err
}
