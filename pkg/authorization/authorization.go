// Package authorization decides what a token subject may do, from the rules
// in the authorization config section. Without a matching rule a request is
// denied.
package authorization

import (
	"fmt"
	"slices"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/authentication"
	"github.com/kubemq-io/kubemq-community/pkg/filewatch"
	"github.com/kubemq-io/kubemq-community/pkg/logging"
	"github.com/pkg/errors"
)

const (
	Send    = "send"
	Receive = "receive"
	Manage  = "manage"
	// Admin is the dashboard and its API, which can do anything, so it is
	// granted on its own rather than per channel.
	Admin = "admin"
)

var ErrPermissionDenied = errors.New("Error 430: permission denied, ")

// Check is one thing a request does: an action on a channel of a type. An
// Admin check has no type or channel.
type Check struct {
	Action  string
	Type    string
	Channel string
}

func (c Check) String() string {
	if c.Action == Admin {
		return Admin
	}
	return fmt.Sprintf("%s on %s channel %q", c.Action, c.Type, c.Channel)
}

type Authorizer struct {
	rules func() []*config.AuthorizationRule
}

// New returns nil when authorization is disabled, so callers can skip the
// check entirely.
func New(cfg *config.AuthorizationConfig) (*Authorizer, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	if cfg.FilePath == "" {
		rules, err := config.ParseAuthorizationRules([]byte(cfg.PolicyData))
		if err != nil {
			return nil, err
		}
		return &Authorizer{rules: func() []*config.AuthorizationRule { return rules }}, nil
	}
	logger := logging.GetLogFactory().NewLogger("authorization")
	file, err := filewatch.New(cfg.FilePath, time.Duration(cfg.AutoReload)*time.Second, config.ParseAuthorizationRules, func(err error) {
		logger.Errorf("error reloading authorization rules, keeping the previous rules: %s", err.Error())
	})
	if err != nil {
		return nil, err
	}
	return &Authorizer{rules: file.Get}, nil
}

// NewWithRules returns an authorizer with fixed rules.
func NewWithRules(rules []*config.AuthorizationRule) *Authorizer {
	return &Authorizer{rules: func() []*config.AuthorizationRule { return rules }}
}

// Allowed reports whether any rule lets the subject do c.
func (a *Authorizer) Allowed(subject string, c Check) bool {
	for _, r := range a.rules() {
		if allows(r, subject, c) {
			return true
		}
	}
	return false
}

// Authorize returns ErrPermissionDenied for the first check the identity is
// not allowed. A request without an identity is denied.
func (a *Authorizer) Authorize(id *authentication.Identity, checks ...Check) error {
	for _, c := range checks {
		if id == nil {
			return fmt.Errorf("%wno identity for %s", ErrPermissionDenied, c)
		}
		if !a.Allowed(id.Subject, c) {
			return fmt.Errorf("%w%s may not %s", ErrPermissionDenied, id.Subject, c)
		}
	}
	return nil
}

func allows(r *config.AuthorizationRule, subject string, c Check) bool {
	if r.Subject != "*" && r.Subject != subject {
		return false
	}
	if !slices.Contains(r.Actions, "*") && !slices.Contains(r.Actions, c.Action) {
		return false
	}
	if c.Action == Admin {
		return true
	}
	if len(r.Types) > 0 && !slices.Contains(r.Types, "*") && !slices.Contains(r.Types, c.Type) {
		return false
	}
	for _, pattern := range r.Channels {
		if MatchChannel(pattern, c.Channel) {
			return true
		}
	}
	return false
}

// MatchChannel matches a channel name against a pattern in which * stands
// for any run of characters, including dots and none at all. A subscription
// to a wildcard channel such as orders.> is matched as written, so it needs a
// pattern that covers that text.
func MatchChannel(pattern, channel string) bool {
	// Iterative glob match with backtracking to the last *.
	p, c := 0, 0
	star, mark := -1, 0
	for c < len(channel) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, c
			p++
		case p < len(pattern) && pattern[p] == channel[c]:
			p++
			c++
		case star >= 0:
			p = star + 1
			mark++
			c = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
