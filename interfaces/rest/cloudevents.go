package rest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	pb "github.com/kubemq-io/protobuf/go"
	"github.com/labstack/echo/v4"
)

// CloudEvents over HTTP (https://github.com/cloudevents/spec), version 1.0:
//
//	POST /cloudevents/<pattern>            the channel is the event's type
//	POST /cloudevents/<pattern>/<channel>
//
// pattern is events, events_store or queues. The request may use any of the
// three HTTP content modes: binary (ce-* headers, the body is the data),
// structured (application/cloudevents+json) or batched
// (application/cloudevents-batch+json, a JSON array of events).
//
// Each event becomes one message: the id is its ID, the source its client ID,
// the type its metadata and the data its body. Every attribute, extensions
// included, is also kept as a tag named ce-<attribute>, so consumers can read
// the event back.

const (
	ceStructuredType = "application/cloudevents+json"
	ceBatchType      = "application/cloudevents-batch+json"
	ceSpecVersion    = "1.0"
	ceTagPrefix      = "ce-"
)

type cloudEvent struct {
	// attributes holds every context attribute except data, by its
	// lowercase name.
	attributes map[string]string
	data       []byte
}

func (e *cloudEvent) validate() error {
	if v := e.attributes["specversion"]; v != ceSpecVersion {
		return fmt.Errorf("unsupported cloudevents specversion %q, only %s is supported", v, ceSpecVersion)
	}
	for _, name := range []string{"id", "source", "type"} {
		if e.attributes[name] == "" {
			return fmt.Errorf("cloudevent attribute %s is required", name)
		}
	}
	return nil
}

func (e *cloudEvent) tags() map[string]string {
	tags := make(map[string]string, len(e.attributes))
	for name, value := range e.attributes {
		tags[ceTagPrefix+name] = value
	}
	return tags
}

// readCloudEvents parses a request in any of the three content modes.
func readCloudEvents(r *http.Request) ([]*cloudEvent, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get(echo.HeaderContentType))
	var events []*cloudEvent
	switch mediaType {
	case ceStructuredType:
		event, err := parseStructuredCloudEvent(body)
		if err != nil {
			return nil, err
		}
		events = []*cloudEvent{event}
	case ceBatchType:
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("invalid cloudevents batch: %w", err)
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("empty cloudevents batch")
		}
		for i, item := range items {
			event, err := parseStructuredCloudEvent(item)
			if err != nil {
				return nil, fmt.Errorf("event %d: %w", i+1, err)
			}
			events = append(events, event)
		}
	default:
		event := parseBinaryCloudEvent(r.Header, body)
		if event.attributes["specversion"] == "" {
			return nil, fmt.Errorf("not a cloudevent: send ce-* headers, or a %s or %s body", ceStructuredType, ceBatchType)
		}
		events = []*cloudEvent{event}
	}
	for _, event := range events {
		if err := event.validate(); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func parseBinaryCloudEvent(header http.Header, body []byte) *cloudEvent {
	event := &cloudEvent{attributes: map[string]string{}, data: body}
	for key, values := range header {
		name := strings.ToLower(key)
		if !strings.HasPrefix(name, ceTagPrefix) || len(values) == 0 {
			continue
		}
		// Header values are percent-encoded where needed.
		value, err := url.PathUnescape(values[0])
		if err != nil {
			value = values[0]
		}
		event.attributes[strings.TrimPrefix(name, ceTagPrefix)] = value
	}
	if contentType := header.Get(echo.HeaderContentType); contentType != "" {
		event.attributes["datacontenttype"] = contentType
	}
	return event
}

func parseStructuredCloudEvent(raw []byte) (*cloudEvent, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("invalid cloudevent: %w", err)
	}
	event := &cloudEvent{attributes: map[string]string{}}
	for name, value := range fields {
		name = strings.ToLower(name)
		if name == "data" || name == "data_base64" {
			continue
		}
		// Attributes are strings, except that extensions may be numbers or
		// booleans, which are kept as written.
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			s = string(value)
		}
		event.attributes[name] = s
	}
	if encoded, ok := fields["data_base64"]; ok {
		var s string
		if err := json.Unmarshal(encoded, &s); err != nil {
			return nil, fmt.Errorf("invalid cloudevent data_base64: %w", err)
		}
		data, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("invalid cloudevent data_base64: %w", err)
		}
		event.data = data
		return event, nil
	}
	data, ok := fields["data"]
	if !ok || bytes.Equal(data, []byte("null")) {
		return event, nil
	}
	// JSON data (the default content type) is kept as JSON. Other data is
	// carried as a JSON string, which is unwrapped.
	if !isJSONContentType(event.attributes["datacontenttype"]) {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			event.data = []byte(s)
			return event, nil
		}
	}
	event.data = data
	return event, nil
}

func isJSONContentType(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || mediaType == "text/json" || strings.HasSuffix(mediaType, "+json")
}

func (s *Server) handlerCloudEvents(c echo.Context) error {
	res := NewResponse(c)
	pattern := c.Param("pattern")
	channel := strings.Trim(c.Param("*"), "/")
	switch pattern {
	case "events", "events_store", "queues":
	default:
		return res.SetErrorWithText(fmt.Sprintf("unknown pattern %q, use events, events_store or queues", pattern)).SetHttpCode(http.StatusNotFound).Send()
	}
	events, err := readCloudEvents(c.Request())
	if err != nil {
		return res.SetError(err).SetHttpCode(http.StatusBadRequest).Send()
	}

	// Build and authorize every message before sending any, so a batch is
	// refused as a whole.
	messages := make([]any, 0, len(events))
	for _, event := range events {
		target := channel
		if target == "" {
			target = event.attributes["type"]
		}
		var msg any
		if pattern == "queues" {
			msg = &pb.QueueMessage{
				MessageID: event.attributes["id"],
				ClientID:  event.attributes["source"],
				Channel:   target,
				Metadata:  event.attributes["type"],
				Body:      event.data,
				Tags:      event.tags(),
			}
		} else {
			msg = &pb.Event{
				EventID:  event.attributes["id"],
				ClientID: event.attributes["source"],
				Channel:  target,
				Metadata: event.attributes["type"],
				Body:     event.data,
				Store:    pattern == "events_store",
				Tags:     event.tags(),
			}
		}
		if err := s.authorize(c, msg, nil); err != nil {
			return denied(res, err)
		}
		messages = append(messages, msg)
	}

	ctx := c.Request().Context()
	results := make([]any, 0, len(messages))
	failed := false
	for _, msg := range messages {
		switch m := msg.(type) {
		case *pb.QueueMessage:
			result, err := s.services.Array.SendQueueMessage(ctx, m)
			if err != nil {
				result = &pb.SendQueueMessageResult{MessageID: m.MessageID, IsError: true, Error: err.Error()}
			}
			failed = failed || result.IsError
			results = append(results, result)
		case *pb.Event:
			var result *pb.Result
			if m.Store {
				result, err = s.services.Array.SendEventsStore(ctx, m)
			} else {
				result, err = s.services.Array.SendEvents(ctx, m)
			}
			if err != nil {
				result = &pb.Result{EventID: m.EventID, Error: err.Error()}
			}
			failed = failed || !result.Sent
			results = append(results, result)
		}
	}
	if failed {
		res.SetErrorWithText("not every event was sent").SetHttpCode(http.StatusInternalServerError)
	} else {
		res.SetHttpCode(http.StatusAccepted)
	}
	if len(results) == 1 {
		return res.SetResponseBody(results[0]).Send()
	}
	return res.SetResponseBody(results).Send()
}
