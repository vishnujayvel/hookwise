package feeds

import "context"

// NewsProducer returns placeholder news items.
type NewsProducer struct{}

func (p *NewsProducer) Name() string { return "news" }
func (p *NewsProducer) Produce(_ context.Context) (interface{}, error) {
	return NewEnvelope("news", map[string]interface{}{
		"stories": []interface{}{
			map[string]interface{}{
				"title": "Placeholder story",
				"url":   "https://example.com",
				"score": 0,
			},
		},
		"source": "placeholder",
	}), nil
}
