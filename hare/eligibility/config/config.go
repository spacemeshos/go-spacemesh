package config

type Config struct {
	ConfidenceParam uint64 `mapstructure:"eligibility-confidence-param"` // the confidence interval
	EpochOffset     int    `mapstructure:"eligibility-epoch-offset"`     // the offset from the beginning of the epoch
}

func DefaultConfig() Config {
	return Config{25, 30}
}
