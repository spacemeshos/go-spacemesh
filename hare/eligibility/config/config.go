package config

// Config is the configuration of the oracle package.
type Config struct {
	ConfidenceParam uint32 `mapstructure:"eligibility-confidence-param"` // the confidence interval
}

// DefaultConfig returns the default configuration for the oracle package.
func DefaultConfig() Config {
	return Config{ConfidenceParam: 1}
}
