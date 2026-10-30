package broker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/atomic"

	"github.com/kubemq-io/kubemq-community/pkg/jstore"
	"github.com/kubemq-io/kubemq-community/pkg/logging"

	"github.com/kubemq-io/kubemq-community/config"

	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	readyTimeout         = 90 * time.Second
	inactiveScanInterval = time.Minute
	adminTimeout         = 10 * time.Second
	maxListedStreams     = 1 << 20
)

type Service struct {
	NatsOptions             *natsd.Options
	isHealthy               *atomic.Bool
	isReady                 *atomic.Bool
	Nats                    *natsd.Server
	Stopped                 chan struct{}
	logger                  *logging.Logger
	appConfig               *config.Config
	cancelFunc              context.CancelFunc
	disableMetricsReporting bool
	stateNotifiers          *HealthNotifier
	adminConn               *nats.Conn
	admin                   jetstream.JetStream
}

func New(appConfig *config.Config) *Service {
	s := &Service{
		Nats:      nil,
		Stopped:   make(chan struct{}, 1),
		appConfig: appConfig,

		isReady:        atomic.NewBool(false),
		isHealthy:      atomic.NewBool(false),
		stateNotifiers: NewHealthNotifier(),
	}
	return s
}
func (s *Service) Start(ctx context.Context) (*Service, error) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel
	s.logger = logging.GetLogFactory().NewLogger("broker")
	err := s.startAsBroker(ctx, s.appConfig)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) DisableMetricsReporting() {
	s.disableMetricsReporting = true
}
func (s *Service) Close() {
	s.cancelFunc()
	s.appConfig.Broker.MemoryPipe.Unbind()
	if s.adminConn != nil {
		s.adminConn.Close()
	}
	s.Nats.Shutdown()
	s.Nats.WaitForShutdown()
	s.logger.Warnw("broker shutdown process completed")
	s.Stopped <- struct{}{}

}

// streamDetails returns the streams and their consumers.
func (s *Service) streamDetails() ([]natsd.StreamDetail, error) {
	if s.Nats == nil {
		return nil, fmt.Errorf("broker is not ready")
	}
	jsz, err := s.Nats.Jsz(&natsd.JSzOptions{Streams: true, Consumer: true, Config: true, Accounts: true, Limit: maxListedStreams})
	if err != nil {
		return nil, err
	}
	var list []natsd.StreamDetail
	for _, account := range jsz.AccountDetails {
		list = append(list, account.Streams...)
	}
	return list, nil
}

func (s *Service) channels(prefix string) (*Queues, error) {
	details, err := s.streamDetails()
	if err != nil {
		return nil, err
	}
	return toQueues(details, prefix), nil
}

func (s *Service) GetQueues(ctx context.Context) (*Queues, error) {
	return s.channels("_QUEUES_.")
}

func (s *Service) GetEventsStores(ctx context.Context) (*Queues, error) {
	return s.channels("_EVENTS_STORE_.")
}

// HasStoreChannel reports whether a stored channel exists. The name includes
// the prefix of its type.
func (s *Service) HasStoreChannel(name string) bool {
	if s.admin == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	_, err := s.admin.Stream(ctx, jstore.StreamName(name))
	return err == nil
}

// DeleteStoreChannel deletes a stored channel with its messages. A channel that
// doesn't exist is not an error, and one with active subscribers isn't deleted.
func (s *Service) DeleteStoreChannel(ctx context.Context, name string) error {
	if s.admin == nil {
		return fmt.Errorf("broker is not ready")
	}
	details, err := s.streamDetails()
	if err != nil {
		return err
	}
	stream := jstore.StreamName(name)
	for _, detail := range details {
		if detail.Name == stream && hasActiveConsumer(detail) {
			return fmt.Errorf("channel has subscribers, it can be deleted when they leave")
		}
	}
	err = s.admin.DeleteStream(ctx, stream)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil
	}
	return err
}

func hasActiveConsumer(detail natsd.StreamDetail) bool {
	for _, consumer := range detail.Consumer {
		if consumer != nil && jstore.ConsumerActive(consumer.NumWaiting, consumer.NumAckPending) {
			return true
		}
	}
	return false
}

// purgeInactiveChannels deletes channels that got no new messages and had no
// subscribers for the given time. Its clock restarts with the broker.
func (s *Service) purgeInactiveChannels(ctx context.Context, inactive time.Duration) {
	started := time.Now()
	ticker := time.NewTicker(inactiveScanInterval)
	defer ticker.Stop()
	lastActive := make(map[string]time.Time)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		details, err := s.streamDetails()
		if err != nil {
			continue
		}
		now := time.Now()
		for _, detail := range details {
			if detail.Config == nil || isInternalStream(detail) {
				continue
			}
			if hasActiveConsumer(detail) {
				lastActive[detail.Name] = now
				continue
			}
			last := lastActive[detail.Name]
			if last.IsZero() {
				last = started
			}
			if detail.Created.After(last) {
				last = detail.Created
			}
			if detail.State.LastTime.After(last) {
				last = detail.State.LastTime
			}
			if now.Sub(last) < inactive {
				continue
			}
			s.logger.Infow("deleting inactive channel", "channel", detail.Config.Subjects)
			if err := s.admin.DeleteStream(ctx, detail.Name); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
				s.logger.Errorw("error deleting inactive channel", "channel", detail.Config.Subjects, "error", err.Error())
				continue
			}
			delete(lastActive, detail.Name)
		}
	}
}

func isInternalStream(detail natsd.StreamDetail) bool {
	for _, subject := range detail.Config.Subjects {
		if strings.HasPrefix(subject, "_QUEUE_DELAY_") {
			return true
		}
	}
	return false
}

func (s *Service) IsHealthy() bool {
	return s.isHealthy.Load()
}
func (s *Service) IsReady() bool {
	return s.isReady.Load()
}

func (s *Service) HealthState() *HealthState {
	return &HealthState{
		IsHealthy: s.isHealthy.Load(),
		IsReady:   s.isReady.Load(),
	}
}

func (s *Service) RegisterToNotifyState(name string, sub func(state bool)) {
	s.stateNotifiers.Register(name, sub)
}
func (s *Service) UnRegisterToNotifyState(name string) {
	s.stateNotifiers.UnRegister(name)
}

type HealthState struct {
	IsHealthy bool `json:"is_healthy"`
	IsReady   bool `json:"is_ready"`
}
