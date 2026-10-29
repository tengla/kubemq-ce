package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kubemq-io/kubemq-community/pkg/api"
	"github.com/kubemq-io/kubemq-community/pkg/api/actions"
	"github.com/kubemq-io/kubemq-community/pkg/authorization"
	pb "github.com/kubemq-io/protobuf/go"
)

// storePrefixes maps the channel types that keep messages in NATS Streaming to
// the prefix of their streaming channel.
var storePrefixes = map[string]string{
	"queues":       "_QUEUES_.",
	"events_store": "_EVENTS_STORE_.",
}

func validChannelType(channelType string) error {
	switch channelType {
	case "events", "events_store", "commands", "queries", "queues":
		return nil
	default:
		return fmt.Errorf("unsupported channel type %q", channelType)
	}
}

// handleInternalRequest answers the channel management queries the SDKs send
// to entities.ClusterInternalRequestsChannel. The channel and its type come
// in the request tags.
func (s *service) handleInternalRequest(ctx context.Context, req *pb.Request) *pb.Response {
	res := &pb.Response{
		RequestID: req.RequestID,
		ClientID:  req.ClientID,
		Timestamp: time.Now().UnixNano(),
	}
	channelType := req.Tags["channel_type"]
	channel := req.Tags["channel"]
	var err error
	switch req.Metadata {
	case "create-channel":
		err = s.actionClient.CreateChannel(ctx, &actions.CreateChannelRequest{Type: channelType, Name: channel})
	case "delete-channel":
		err = s.deleteChannel(ctx, channelType, channel)
	case "list-channels":
		var list []*api.ChannelInfo
		list, err = s.listChannels(ctx, channelType, req.Tags["channel_search"])
		if err == nil {
			res.Body, err = json.Marshal(list)
		}
	default:
		err = fmt.Errorf("unknown request %q", req.Metadata)
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Executed = true
	return res
}

// listChannels lists the channels of one type whose name contains search
// and that the caller may manage. It takes a fresh snapshot, so a channel
// created just before shows up.
func (s *service) listChannels(ctx context.Context, channelType, search string) ([]*api.ChannelInfo, error) {
	if err := validChannelType(channelType); err != nil {
		return nil, err
	}
	ss, err := s.getCurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.channelInfos(ctx, ss, channelType, search), nil
}

// channelInfos lists the channels of one type in a snapshot whose name
// contains search and that the caller may manage.
func (s *service) channelInfos(ctx context.Context, ss *api.Snapshot, channelType, search string) []*api.ChannelInfo {
	list := []*api.ChannelInfo{}
	family, ok := ss.Entities["channels"].GetFamily(channelType)
	if !ok {
		return list
	}
	for _, entity := range family.List() {
		if entity.Name == "" || !strings.Contains(entity.Name, search) {
			continue
		}
		if !s.guard.Allowed(ctx, authorization.Check{Action: authorization.Manage, Type: channelType, Channel: entity.Name}) {
			continue
		}
		list = append(list, api.NewChannelInfo(channelType, entity))
	}
	return list
}

// deleteChannel deletes a channel's stored messages, if its type keeps any,
// and its statistics. A channel that still has clients shows up again with
// their next message.
func (s *service) deleteChannel(ctx context.Context, channelType, channel string) error {
	if err := validChannelType(channelType); err != nil {
		return err
	}
	if channel == "" {
		return fmt.Errorf("channel not set")
	}
	ss, err := s.getCurrentSnapshot(ctx)
	if err != nil {
		return err
	}
	_, found := ss.Entities["channels"].GetEntity(channelType, channel)
	if prefix, ok := storePrefixes[channelType]; ok {
		if s.broker.Snats != nil && s.broker.Snats.HasChannel(prefix+channel) {
			found = true
		}
		if err := s.broker.DeleteStoreChannel(ctx, prefix+channel); err != nil {
			return fmt.Errorf("error deleting %s channel %s: %s", channelType, channel, err.Error())
		}
	}
	if !found {
		return fmt.Errorf("%s channel %s not found", channelType, channel)
	}
	s.metricsExporter.DeleteChannel(channelType, channel)
	s.Lock()
	s.lastLoadedEntitiesGroups["channels"].RemoveEntity(channelType, channel)
	s.lastLoadedEntitiesGroups["clients"].RemoveFamily(fmt.Sprintf("%s/%s", channelType, channel))
	s.Unlock()
	// Save right away, so the channel doesn't come back after a restart.
	s.saveEntitiesGroup(ctx)
	return nil
}
