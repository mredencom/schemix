package schemixotel

type config struct {
	meterName string
}

// Option configures the Recorder.
type Option func(*config)

func defaultConfig() *config {
	return &config{}
}

// WithMeterName sets a custom OTel meter name.
// Default: schemix.ScopeName ("github.com/mredencom/schemix").
func WithMeterName(name string) Option {
	return func(cfg *config) {
		cfg.meterName = name
	}
}
