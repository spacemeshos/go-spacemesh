package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	Kappa                      uint64  `mapstructure:"tortoise-beacon-kappa"`                          // Security parameter (for calculating ATX threshold)
	Q                          string  `mapstructure:"tortoise-beacon-q"`                              // Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.
	RoundsNumber               uint64  `mapstructure:"tortoise-beacon-rounds-number"`                  // Amount of rounds in every epoch
	GracePeriodDurationMs      int     `mapstructure:"tortoise-beacon-grace-period-duration-ms"`       // Grace period duration in milliseconds
	ProposalDurationMs         int     `mapstructure:"tortoise-beacon-proposal-duration-ms"`           // Proposal phase duration in milliseconds
	FirstVotingRoundDurationMs int     `mapstructure:"tortoise-beacon-first-voting-round-duration-ms"` // First voting round duration in milliseconds
	VotingRoundDurationMs      int     `mapstructure:"tortoise-beacon-voting-round-duration-ms"`       // Voting round duration in milliseconds
	WeakCoinRoundDurationMs    int     `mapstructure:"tortoise-beacon-weak-coin-round-duration-ms"`    // Weak coin round duration in milliseconds
	Theta                      float64 `mapstructure:"tortoise-beacon-theta"`                          // Ratio of votes for reaching consensus
	VotesLimit                 int     `mapstructure:"tortoise-beacon-votes-limit"`                    // Maximum allowed number of votes to be sent
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		Kappa:                      40,
		Q:                          "1/3",
		RoundsNumber:               300,
		GracePeriodDurationMs:      2 * 60 * 1000,      // 2 minutes
		ProposalDurationMs:         2 * 60 * 1000,      // 2 minutes
		FirstVotingRoundDurationMs: 1 * 60 * 60 * 1000, // 1 hour
		VotingRoundDurationMs:      30 * 60 * 1000,     // 30 minutes
		WeakCoinRoundDurationMs:    1 * 60 * 1000,      // 1 minute
		Theta:                      0.25,
		VotesLimit:                 100, // TODO: around 100, find the calculation in the forum
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		Kappa:                      400000,
		Q:                          "1/3",
		RoundsNumber:               2,
		GracePeriodDurationMs:      20,
		ProposalDurationMs:         20,
		FirstVotingRoundDurationMs: 40,
		VotingRoundDurationMs:      20,
		WeakCoinRoundDurationMs:    20,
		Theta:                      0.00004,
		VotesLimit:                 100,
	}
}
