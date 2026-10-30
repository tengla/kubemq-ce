// Package pipe connects the clients in this process to the embedded broker.
package pipe

import (
	"errors"
	"net"
	"sync"
)

// ConnProvider is what the embedded NATS server offers for in-process
// connections.
type ConnProvider interface {
	InProcessConn() (net.Conn, error)
}

// Limits are the per-stream limits the broker's configuration sets. Zero means
// no limit.
type Limits struct {
	MaxAgeMinutes int
	MaxBytes      int64
	MaxMsgs       int64
	// ConsumerInactiveMinutes is how long the position of an events store
	// subscriber is kept while it's away.
	ConsumerInactiveMinutes int
}

// Pipe is the handle clients use to reach the broker. It exists before the
// broker starts, so config can hold it, and works once the broker binds to it.
type Pipe struct {
	id       string
	mu       sync.Mutex
	provider ConnProvider
	limits   Limits
	clients  map[string]struct{}
}

func NewPipe(id string) *Pipe {
	return &Pipe{id: id, clients: make(map[string]struct{})}
}

// Bind makes the pipe connect to a started broker.
func (p *Pipe) Bind(provider ConnProvider, limits Limits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provider = provider
	p.limits = limits
}

// Unbind disconnects the pipe from a broker that is shutting down.
func (p *Pipe) Unbind() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provider = nil
}

// InProcessConn implements nats.InProcessConnProvider.
func (p *Pipe) InProcessConn() (net.Conn, error) {
	p.mu.Lock()
	provider := p.provider
	p.mu.Unlock()
	if provider == nil {
		return nil, errors.New("broker is not running")
	}
	return provider.InProcessConn()
}

// Limits returns the limits new streams get.
func (p *Pipe) Limits() Limits {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.limits
}

// RegisterClient reserves a client ID. It returns false if a connected client
// has it already.
func (p *Pipe) RegisterClient(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.clients[id]; ok {
		return false
	}
	p.clients[id] = struct{}{}
	return true
}

// ReleaseClient frees a client ID.
func (p *Pipe) ReleaseClient(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.clients, id)
}
