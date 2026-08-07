package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/gocql/gocql"
	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
)

// MessageEnvelope is the data flowing through the processing pipeline.
type MessageEnvelope struct {
	MessageID       string            `json:"messageId"`
	MessageType     string            `json:"messageType"`
	TriggerEvent    string            `json:"triggerEvent"`
	SendingFacility string            `json:"sendingFacility"`
	PatientID       string            `json:"patientId"`
	RawPayload      string            `json:"rawPayload"`
	Properties      map[string]string `json:"properties"`
}

// ProcessResult is returned by a processor plugin after handling a message.
type ProcessResult struct {
	DestinationTopic   string
	DestCommPointIDs   []string // All destination CPs (primary + fan-out)
	TransformedPayload string   // rawPayload content for wire delivery
	Envelope           *MessageEnvelope // Full envelope for DB audit trail
	OrgID              string   // Owning org from the matched route
	RouteID            string   // Matched route ID
	SourceCPID         string   // Source comm point ID from the matched route
	Err                error
}

// Processor is a plugin that handles message processing (routes, filters, transforms).
type Processor interface {
	// Name returns the plugin identifier.
	Name() string

	// Init is called once at startup with dependencies.
	Init(ctx context.Context, deps *Dependencies) error

	// Process runs a message through the plugin's pipeline.
	// If the plugin has no opinion (e.g. no matching route), it returns passthrough=true.
	Process(ctx context.Context, envelope *MessageEnvelope) (result ProcessResult, passthrough bool)

	// RegisterAPIRoutes adds any REST endpoints this plugin exposes.
	RegisterAPIRoutes(router fiber.Router)

	// RegisterNATSHandlers subscribes to any NATS subjects this plugin needs.
	RegisterNATSHandlers(nc *nats.Conn)

	// Close releases plugin resources.
	Close() error
}

// Dependencies holds shared infrastructure passed to plugins at init.
type Dependencies struct {
	Session *gocql.Session
	NC      *nats.Conn
	JS      nats.JetStreamContext
}

// Registry holds all registered plugins and dispatches messages through them.
type Registry struct {
	mu         sync.RWMutex
	processors []Processor
}

// NewRegistry creates an empty plugin registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a processor plugin to the registry.
func (r *Registry) Register(p Processor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.processors = append(r.processors, p)
}

// InitAll initializes all registered plugins.
func (r *Registry) InitAll(ctx context.Context, deps *Dependencies) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.processors {
		if err := p.Init(ctx, deps); err != nil {
			return fmt.Errorf("plugin %s init: %w", p.Name(), err)
		}
	}
	return nil
}

// Process runs a message through all processor plugins in order.
// The first plugin that does not pass-through wins.
// If all pass-through, the raw envelope is returned on the "default" topic.
func (r *Registry) Process(ctx context.Context, envelope *MessageEnvelope) ProcessResult {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.processors {
		result, passthrough := p.Process(ctx, envelope)
		if !passthrough {
			return result
		}
	}

	// No plugin claimed it — default passthrough
	return ProcessResult{
		DestinationTopic:   "default",
		TransformedPayload: envelope.RawPayload,
	}
}

// RegisterAllAPIRoutes registers all plugin API routes under the given router.
func (r *Registry) RegisterAllAPIRoutes(router fiber.Router) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.processors {
		p.RegisterAPIRoutes(router)
	}
}

// RegisterAllNATSHandlers registers all plugin NATS handlers.
func (r *Registry) RegisterAllNATSHandlers(nc *nats.Conn) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.processors {
		p.RegisterNATSHandlers(nc)
	}
}

// CloseAll shuts down all plugins.
func (r *Registry) CloseAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.processors {
		p.Close()
	}
}

// Processors returns the list of registered processors (for introspection).
func (r *Registry) Processors() []Processor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Processor, len(r.processors))
	copy(result, r.processors)
	return result
}
