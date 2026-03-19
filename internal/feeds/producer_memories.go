package feeds

import "context"

// MemoriesProducer returns placeholder memories.
type MemoriesProducer struct{}

func (p *MemoriesProducer) Name() string { return "memories" }
func (p *MemoriesProducer) Produce(_ context.Context) (interface{}, error) {
	return NewEnvelope("memories", map[string]interface{}{
		"recent_memories": []interface{}{},
		"total_count":     0,
		"source":          "placeholder",
	}), nil
}
