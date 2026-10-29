package config

import (
	"fmt"
	"slices"

	"github.com/ghodss/yaml"
)

// AuthorizationConfig limits what each token subject may do. Rules come from
// PolicyData, as YAML or JSON, or from the file at FilePath, which is checked
// for changes every AutoReload seconds when that is above 0. It needs
// authentication, which provides the subjects.
type AuthorizationConfig struct {
	Enable     bool   `json:"enable"`
	PolicyData string `json:"policyData"`
	FilePath   string `json:"filePath"`
	AutoReload int    `json:"autoReload"`
}

// AuthorizationRule allows a subject some actions on some channels. A request
// is allowed when any rule allows it.
type AuthorizationRule struct {
	// Subject is a token's sub claim, or * for every subject.
	Subject string `json:"subject"`
	// Channels are channel names in which * matches any run of characters,
	// so orders.* matches orders.created and orders.eu.created.
	Channels []string `json:"channels"`
	// Types limits the rule to some channel types; empty means all.
	Types []string `json:"types,omitempty"`
	// Actions are send, receive, manage (create, delete and list channels),
	// admin (the dashboard, which can do anything) or * for all of them.
	Actions []string `json:"actions"`
}

var (
	AuthorizationActions = []string{"send", "receive", "manage", "admin"}
	ChannelTypes         = []string{"events", "events_store", "commands", "queries", "queues"}
)

// ParseAuthorizationRules parses and checks a YAML or JSON list of rules.
func ParseAuthorizationRules(data []byte) ([]*AuthorizationRule, error) {
	var rules []*AuthorizationRule
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("invalid authorization rules: %w", err)
	}
	for i, r := range rules {
		if err := r.validate(); err != nil {
			return nil, fmt.Errorf("authorization rule %d: %w", i+1, err)
		}
	}
	return rules, nil
}

func (r *AuthorizationRule) validate() error {
	if r == nil {
		return fmt.Errorf("empty rule")
	}
	if r.Subject == "" {
		return fmt.Errorf("subject not set")
	}
	if len(r.Actions) == 0 {
		return fmt.Errorf("actions not set")
	}
	onlyAdmin := true
	for _, a := range r.Actions {
		if a != "*" && !slices.Contains(AuthorizationActions, a) {
			return fmt.Errorf("unknown action %q", a)
		}
		if a != "admin" {
			onlyAdmin = false
		}
	}
	if len(r.Channels) == 0 && !onlyAdmin {
		return fmt.Errorf("channels not set")
	}
	for _, t := range r.Types {
		if t != "*" && !slices.Contains(ChannelTypes, t) {
			return fmt.Errorf("unknown channel type %q", t)
		}
	}
	return nil
}

func defaultAuthorizationConfig() *AuthorizationConfig {

	bindViperEnv(
		"Authorization.Enable",
		"Authorization.PolicyData",
		"Authorization.FilePath",
		"Authorization.AutoReload",
	)

	return &AuthorizationConfig{
		Enable:     false,
		PolicyData: "",
		FilePath:   "",
		AutoReload: 0,
	}
}

func (a *AuthorizationConfig) Validate() error {
	if !a.Enable {
		return nil
	}
	if a.AutoReload < 0 {
		return NewConfigurationError("invalid auto reload time, cannot be negative")
	}
	switch {
	case a.PolicyData != "" && a.FilePath != "":
		return NewConfigurationError("bad authorization configuration: set either policyData or filePath, not both")
	case a.PolicyData != "":
		if _, err := ParseAuthorizationRules([]byte(a.PolicyData)); err != nil {
			return NewConfigurationErrorf("bad authorization configuration: %s", err.Error())
		}
	case a.FilePath != "":
		if err := validateFileName(a.FilePath); err != nil {
			return NewConfigurationErrorf("bad authorization configuration: %s", err.Error())
		}
	default:
		return NewConfigurationError("missing authorization configuration parameters")
	}
	return nil
}
