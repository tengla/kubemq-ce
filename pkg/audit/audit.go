// Package audit writes one JSON line per client request: who did what on
// which channel, and whether it was allowed.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/kubemq-io/kubemq-community/config"
	"github.com/kubemq-io/kubemq-community/pkg/logging"
)

const (
	Allowed         = "allowed"
	Denied          = "denied"
	Unauthenticated = "unauthenticated"
)

type Entry struct {
	Time time.Time `json:"time"`
	// Transport is grpc, rest or api (the dashboard).
	Transport string `json:"transport"`
	Remote    string `json:"remote,omitempty"`
	Subject   string `json:"subject,omitempty"`
	TokenID   string `json:"tokenId,omitempty"`
	ClientID  string `json:"clientId,omitempty"`
	// Operation is the gRPC method, REST path or dashboard action.
	Operation string `json:"operation"`
	Action    string `json:"action,omitempty"`
	Type      string `json:"type,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Outcome   string `json:"outcome"`
	Error     string `json:"error,omitempty"`
}

type Logger struct {
	mu     sync.Mutex
	out    io.WriteCloser
	logger *logging.Logger
}

// New returns nil when the audit log is disabled; Log on a nil Logger does
// nothing.
func New(cfg *config.AuditConfig) (*Logger, error) {
	if cfg == nil || !cfg.Enable {
		return nil, nil
	}
	l := &Logger{logger: logging.GetLogFactory().NewLogger("audit")}
	if cfg.FilePath != "" {
		// Each line goes out in a single append, so several loggers can
		// share the file.
		f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("error opening audit log: %w", err)
		}
		l.out = f
	}
	return l, nil
}

func (l *Logger) Log(e *Entry) {
	if l == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	if l.out == nil {
		l.logger.Info(string(line))
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.out.Write(append(line, '\n')); err != nil {
		l.logger.Errorf("error writing audit log: %s", err.Error())
	}
}

func (l *Logger) Close() {
	if l == nil || l.out == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.out.Close()
}
