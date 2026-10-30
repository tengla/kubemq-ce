package broker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/entities"
	"github.com/kubemq-io/kubemq-community/pkg/jstore"
	"github.com/kubemq-io/kubemq-community/pkg/pipe"
	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const minMaxPending = 200 * 1024 * 1024

func (s *Service) getBrokerOptions(appConfig *config.Config) *natsd.Options {
	opts := &natsd.Options{
		// Clients are all in this process, so nothing listens on a port.
		DontListen:    true,
		NoSigs:        true,
		WriteDeadline: time.Duration(appConfig.Broker.WriteDeadline) * time.Millisecond,
		MaxConn:       appConfig.Broker.MaxConn,
		MaxPayload:    int32(appConfig.Broker.MaxPayload),
		MaxPending:    minMaxPending,

		JetStream:          true,
		StoreDir:           filepath.Join(appConfig.Store.StorePath, appConfig.Host),
		JetStreamMaxStore:  appConfig.Store.MaxStorage,
		JetStreamMaxMemory: appConfig.Store.MaxMemory,
		SyncInterval:       time.Duration(appConfig.Broker.DiskSyncSeconds) * time.Second,
	}
	if int64(opts.MaxPayload) > opts.MaxPending {
		opts.MaxPending = int64(opts.MaxPayload)
	}
	return opts
}

func (s *Service) startAsBroker(ctx context.Context, appConfig *config.Config) error {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Fatalf("recovering from panic, %v", r)
		}
	}()
	if appConfig.Store.CleanStore {
		_ = os.RemoveAll(appConfig.Store.StorePath)
	}

	s.NatsOptions = s.getBrokerOptions(appConfig)
	s.warnAboutOldStore(s.NatsOptions.StoreDir)
	ns, err := natsd.NewServer(s.NatsOptions)
	if err != nil {
		return err
	}
	ns.SetLogger(s.logger, appConfig.Log.Level == "debug", false)

	go ns.Start()
	if !ns.ReadyForConnections(readyTimeout) {
		ns.Shutdown()
		return entities.ErrServerIsNotReady
	}
	s.Nats = ns
	if appConfig.Store.MaxQueues > 0 {
		// One more for the channel that holds delayed queue messages.
		err := ns.GlobalAccount().UpdateJetStreamLimits(map[string]natsd.JetStreamAccountLimits{
			"": {MaxMemory: -1, MaxStore: -1, MaxConsumers: -1, MaxStreams: appConfig.Store.MaxQueues + 1},
		})
		if err != nil {
			ns.Shutdown()
			return err
		}
	}

	appConfig.Broker.MemoryPipe.Bind(ns, pipe.Limits{
		MaxAgeMinutes:           appConfig.Store.MaxRetention,
		MaxBytes:                appConfig.Store.MaxQueueSize,
		MaxMsgs:                 int64(appConfig.Store.MaxMessages),
		ConsumerInactiveMinutes: appConfig.Store.MaxRetention,
	})
	s.adminConn, err = nats.Connect("", nats.InProcessServer(ns), nats.Name("broker-admin"))
	if err != nil {
		ns.Shutdown()
		return err
	}
	s.admin, err = jetstream.New(s.adminConn)
	if err != nil {
		s.adminConn.Close()
		ns.Shutdown()
		return err
	}
	resetCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if err := jstore.ResetQueueConsumers(resetCtx, s.admin); err != nil {
		s.adminConn.Close()
		ns.Shutdown()
		return err
	}
	if inactive := time.Duration(appConfig.Store.MaxPurgeInactive) * time.Minute; inactive > 0 {
		go s.purgeInactiveChannels(ctx, inactive)
	}

	s.isHealthy.Store(true)
	s.isReady.Store(true)
	return nil
}

// warnAboutOldStore tells whoever upgraded that NATS Streaming's files aren't
// read anymore. They are left in place.
func (s *Service) warnAboutOldStore(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "server.dat")); err == nil {
		s.logger.Warnw(fmt.Sprintf("found NATS Streaming data in %s; this version stores messages in JetStream and doesn't read it, so messages from before the upgrade are not available", dir))
	}
}
