package deeplink

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Processor defines how a link type is handled during generation.
type Processor interface {
	// Type returns the link type identifier (e.g. "redirect").
	Type() string
	// Process populates the payload (URL, Title, etc.) during link generation.
	Process(ctx context.Context, payload *Link) error
}

// Previewer is optionally implemented by processors that return custom
// template data. If not implemented, the payload itself is used.
type Previewer interface {
	Preview(payload *Link) any
}

// Registry manages processors by type name.
type Registry struct {
	mu         sync.RWMutex
	processors map[string]Processor
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{processors: make(map[string]Processor)}
}

// Register adds a processor. Panics if the type is already registered.
func (r *Registry) Register(p Processor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := p.Type()
	if _, exists := r.processors[t]; exists {
		panic(fmt.Sprintf("deeplink: type %q already registered", t))
	}
	r.processors[t] = p
}

// Get returns the processor for linkType, or nil.
func (r *Registry) Get(linkType string) Processor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.processors[linkType]
}

// Types returns all registered type names.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.processors))
	for t := range r.processors {
		types = append(types, t)
	}
	slices.Sort(types)
	return types
}
