package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	Kappa                 int     `mapstructure:"tortoise-beacon-kappa"`                        // Security parameter (for calculating ATX threshold)
	Q                     float64 `mapstructure:"tortoise-beacon-q"`                            // Ratio of dishonest spacetime (for calculating ATX threshold)
	RoundsNumber          uint64  `mapstructure:"tortoise-beacon-rounds-number"`                // Amount of rounds in every epoch
	VotingRoundDuration   int     `mapstructure:"tortoise-beacon-voting-round-duration-sec"`    // Round duration in seconds
	WeakCoinRoundDuration int     `mapstructure:"tortoise-beacon-weak-coin-round-duration-sec"` // Round duration in seconds
	Theta                 float64 `mapstructure:"tortoise-beacon-theta"`                        // Ratio of votes for reaching consensus
	VotesLimit            int     `mapstructure:"tortoise-beacon-votes-limit"`                  // Maximum allowed number of votes to be sent
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		Kappa:                 40,
		Q:                     1.0 / 3.0,
		RoundsNumber:          300,
		VotingRoundDuration:   15 * 60, // 15 minutes // TODO(nkryuchkov): change
		WeakCoinRoundDuration: 15 * 60, // 15 minutes // TODO(nkryuchkov): change
		Theta:                 0.25,
		VotesLimit:            100, // TODO(nkryuchkov): change
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		Kappa:                 40,
		Q:                     1.0 / 3.0,
		RoundsNumber:          2,
		VotingRoundDuration:   1,
		WeakCoinRoundDuration: 1,
		Theta:                 1,
		VotesLimit:            100,
	}
}
