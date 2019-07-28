package config

type Config struct {
	ConfidenceParam  uint64 `mapstructure:"eligibility-confidence-param"`    // the confidence interval
	GenesisActiveSet int    `mapstructure:"eligibility-genesis-active-size"` // the active set size for genesis
	EpochOffset      int    `mapstructure:"eligibility-epoch-offset"`        // the offset from the beginning of the epoch
}

func DefaultConfig() Config {
	return Config{25, 5, 30}
}
