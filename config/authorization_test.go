package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_AuthorizationLoadEnvironmentVars(t *testing.T) {
	authConfig := &AuthorizationConfig{
		Enable:     true,
		PolicyData: "some-env-data",
		FilePath:   "some-env-file-path",
		AutoReload: 1000,
	}
	authEnv := map[string]string{
		"Authorization.Enable":     "true",
		"Authorization.PolicyData": "some-env-data",
		"Authorization.FilePath":   "some-env-file-path",
		"Authorization.AutoReload": "1000",
	}
	setEnvValues(authEnv)
	appConfig := getConfigRecord()
	require.EqualValues(t, authConfig, appConfig.Authorization)
}

const validRules = `
- subject: orders-service
  channels: ["orders.*"]
  types: [queues, events]
  actions: [send, receive]
- subject: ops
  actions: [admin]
`

func TestAuthorizationConfig_Validate(t *testing.T) {
	rulesFile := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(rulesFile, []byte(validRules), 0o600))

	tests := []struct {
		name    string
		cfg     *AuthorizationConfig
		wantErr bool
	}{
		{"default", defaultAuthorizationConfig(), false},
		{"policy", &AuthorizationConfig{Enable: true, PolicyData: validRules}, false},
		{"policy as json", &AuthorizationConfig{Enable: true, PolicyData: `[{"subject":"a","channels":["*"],"actions":["*"]}]`}, false},
		{"file", &AuthorizationConfig{Enable: true, FilePath: rulesFile, AutoReload: 10}, false},
		{"empty", &AuthorizationConfig{Enable: true}, true},
		{"both", &AuthorizationConfig{Enable: true, PolicyData: validRules, FilePath: rulesFile}, true},
		{"missing file", &AuthorizationConfig{Enable: true, FilePath: "as"}, true},
		{"bad policy", &AuthorizationConfig{Enable: true, PolicyData: "some-policy"}, true},
		{"bad auto reload", &AuthorizationConfig{Enable: true, FilePath: rulesFile, AutoReload: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAuthorizationRules(t *testing.T) {
	rules, err := ParseAuthorizationRules([]byte(validRules))
	require.NoError(t, err)
	require.Len(t, rules, 2)
	require.Equal(t, &AuthorizationRule{Subject: "orders-service", Channels: []string{"orders.*"}, Types: []string{"queues", "events"}, Actions: []string{"send", "receive"}}, rules[0])

	bad := map[string]string{
		"no subject":       `[{"channels":["a"],"actions":["send"]}]`,
		"no actions":       `[{"subject":"a","channels":["a"]}]`,
		"no channels":      `[{"subject":"a","actions":["send"]}]`,
		"unknown action":   `[{"subject":"a","channels":["a"],"actions":["publish"]}]`,
		"unknown type":     `[{"subject":"a","channels":["a"],"actions":["send"],"types":["topics"]}]`,
		"not a list":       `subject: a`,
		"null rule":        `[null]`,
		"admin + channels": `[{"subject":"a","actions":["admin","send"]}]`,
	}
	for name, data := range bad {
		t.Run(name, func(t *testing.T) {
			_, err := ParseAuthorizationRules([]byte(data))
			require.Error(t, err)
		})
	}
}
