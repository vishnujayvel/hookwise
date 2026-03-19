package feeds

// RegisterBuiltins registers all 6 built-in feed producers with the registry.
func RegisterBuiltins(r *Registry) {
	r.Register(&ProjectProducer{})
	r.Register(&NewsProducer{})
	r.Register(&CalendarProducer{})
	r.Register(&WeatherProducer{})
	r.Register(&MemoriesProducer{})
	r.Register(&InsightsProducer{})
}
