package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct { // TODO(nkryuchkov): use unused fields
	Kappa         int     `mapstructure:"tortoise-beacon-kappa"`
	Q             float64 `mapstructure:"tortoise-beacon-q"`
	RoundsNumber  uint64  `mapstructure:"tortoise-beacon-rounds-number"`
	RoundDuration int     `mapstructure:"tortoise-beacon-round-duration-sec"`
	Theta         float64 `mapstructure:"tortoise-beacon-theta"`
	TAve          int     `mapstructure:"tortoise-beacon-t-ave"`
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		Kappa:         40,
		Q:             1.0 / 3.0,
		RoundsNumber:  300,
		RoundDuration: 30 * 60, // 30 minutes
		Theta:         0.25,
		TAve:          1, // TODO(nkryuchkov): change
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		Kappa:         40,
		Q:             1.0 / 3.0,
		RoundsNumber:  2,
		RoundDuration: 1,
		Theta:         1,
		TAve:          1,
	}
}
