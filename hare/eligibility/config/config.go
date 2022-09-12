package config

// Config is the configuration of the oracle package.
type Config struct {
	ConfidenceParam uint32 `mapstructure:"eligibility-confidence-param"` // the confidence interval
	EpochOffset     uint32 `mapstructure:"eligibility-epoch-offset"`     // the offset from the beginning of the epoch
}

// DefaultConfig returns the default configuration for the oracle package.
func DefaultConfig() Config {
	return Config{ConfidenceParam: 25, EpochOffset: 0}
}
