// Package feeds implements the feed platform: daemon process management,
// goroutine-per-feed polling, built-in and custom feed producers.
package feeds

import (
	"context"
	"sync"
)

// Producer is the interface for feed data producers.
type Producer interface {
	// Name returns the unique name of this feed producer.
	Name() string
	// Produce fetches fresh data for this feed. The returned value is
	// JSON-serialisable and will be written to the feed cache.
	Produce(ctx context.Context) (interface{}, error)
}

// Registry holds registered producers keyed by name.
type Registry struct {
	mu        sync.RWMutex
	producers map[string]Producer
}

// NewRegistry creates an empty producer registry.
func NewRegistry() *Registry {
	return &Registry{
		producers: make(map[string]Producer),
	}
}

// Register adds a producer to the registry, keyed by its Name().
func (r *Registry) Register(p Producer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.producers == nil {
		r.producers = make(map[string]Producer)
	}
	r.producers[p.Name()] = p
}

// Get retrieves a producer by name.
func (r *Registry) Get(name string) (Producer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.producers[name]
	return p, ok
}

// All returns all registered producers in no guaranteed order.
func (r *Registry) All() []Producer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Producer, 0, len(r.producers))
	for _, p := range r.producers {
		result = append(result, p)
	}
	return result
}
