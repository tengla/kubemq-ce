package config

import (
	"github.com/kubemq-io/kubemq-community/pkg/pipe"
	"github.com/nats-io/nuid"
)

// set Global max size for messagego
var globalMaxBodySizeDefault = 1024 * 1024 * 100

type BrokerConfig struct {
	MaxPayload      int        `json:"maxPayload"`
	WriteDeadline   int        `json:"writeDeadline"`
	MaxConn         int        `json:"maxConn"`
	MemoryPipe      *pipe.Pipe `json:"-"`
	DiskSyncSeconds int        `json:"diskSyncSeconds"`
}

func defaultBrokerConfig() *BrokerConfig {
	bindViperEnv(
		"Broker.MaxPayload",
		"Broker.WriteDeadline",
		"Broker.MaxConn",
		"Broker.DiskSyncSeconds",
	)
	return &BrokerConfig{
		MaxPayload:      globalMaxBodySizeDefault,
		WriteDeadline:   2000,
		MaxConn:         0,
		MemoryPipe:      pipe.NewPipe(nuid.Next()),
		DiskSyncSeconds: 60,
	}
}

func (b *BrokerConfig) Validate() error {
	if b.MaxPayload < 0 {
		return NewConfigurationError("bad broker configuration: MaxPayload can not be negative")
	}
	if b.WriteDeadline < 0 {
		return NewConfigurationError("bad broker configuration: WriteDeadline can not be negative")
	}
	if b.MaxConn < 0 {
		return NewConfigurationError("bad broker configuration: MaxConn can not be negative")
	}
	if b.DiskSyncSeconds < 0 {
		return NewConfigurationError("bad broker configuration: DiskSyncSeconds can not be negative")
	}
	return nil
}

func BrokerName() string {
	return "kubemq"
}
