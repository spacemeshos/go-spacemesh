package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	K           int `mapstructure:"tortoise-beacon-rounds-number"` // number of rounds
	WakeupDelta int `mapstructure:"tortoise-beacon-wakeup-delta"`  // the wakeup delta after tick
}

// DefaultConfig returns the default configuration for the hare.
func DefaultConfig() Config {
	return Config{
		K:           6,
		WakeupDelta: 10,
	}
}
