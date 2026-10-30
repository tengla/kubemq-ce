package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_BrokerVarsLoadEnvironmentVars(t *testing.T) {
	c := &BrokerConfig{
		MaxPayload:      2,
		WriteDeadline:   2,
		MaxConn:         2,
		MemoryPipe:      nil,
		DiskSyncSeconds: 60,
	}
	cEnv := map[string]string{
		"Broker.MaxPayload":      "2",
		"Broker.WriteDeadline":   "2",
		"Broker.MaxConn":         "2",
		"Broker.DiskSyncSeconds": "60",
	}
	setEnvValues(cEnv)
	appConfig := getConfigRecord()

	c.MemoryPipe = appConfig.Broker.MemoryPipe
	require.EqualValues(t, c, appConfig.Broker)

}

func TestBrokerConfig_Validate(t *testing.T) {

	tests := []struct {
		name    string
		cfg     *BrokerConfig
		wantErr bool
	}{
		{
			name:    "disable",
			cfg:     defaultBrokerConfig(),
			wantErr: false,
		},
		{
			name: "proper_config",
			cfg: &BrokerConfig{
				MaxPayload:      1,
				WriteDeadline:   1,
				MaxConn:         1,
				MemoryPipe:      nil,
				DiskSyncSeconds: 60,
			},
			wantErr: false,
		},
		{
			name: "bad_payload",
			cfg: &BrokerConfig{

				MaxPayload:    -1,
				WriteDeadline: 1,
				MaxConn:       1,
			},
			wantErr: true,
		},
		{
			name: "bad_Deadline",
			cfg: &BrokerConfig{

				MaxPayload:    1,
				WriteDeadline: -1,
				MaxConn:       1,
			},
			wantErr: true,
		},
		{
			name: "bad_MaxCon",
			cfg: &BrokerConfig{

				MaxPayload:    1,
				WriteDeadline: 1,
				MaxConn:       -1,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
