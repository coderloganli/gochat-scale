package tracing

// Config holds the configuration for distributed tracing.
type Config struct {
	Enabled      bool    `mapstructure:"enabled"`
	Endpoint     string  `mapstructure:"endpoint"`
	SamplingRate float64 `mapstructure:"samplingRate"`
}

// DefaultConfig returns a default tracing configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		Endpoint:     "localhost:4318",
		SamplingRate: 1.0,
	}
}
