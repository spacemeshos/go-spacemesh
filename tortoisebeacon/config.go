package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	Kappa                       uint64  `mapstructure:"tortoise-beacon-kappa"`                           // Security parameter (for calculating ATX threshold)
	Q                           string  `mapstructure:"tortoise-beacon-q"`                               // Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.
	RoundsNumber                uint64  `mapstructure:"tortoise-beacon-rounds-number"`                   // Amount of rounds in every epoch
	GracePeriodDurationSec      int     `mapstructure:"tortoise-beacon-grace-period-duration-sec"`       // Grace period duration in seconds
	ProposalDurationSec         int     `mapstructure:"tortoise-beacon-proposal-duration-sec"`           // Proposal phase duration in seconds
	FirstVotingRoundDurationSec int     `mapstructure:"tortoise-beacon-first-voting-round-duration-sec"` // First voting round duration in seconds
	VotingRoundDurationSec      int     `mapstructure:"tortoise-beacon-voting-round-duration-sec"`       // Voting round duration in seconds
	WeakCoinRoundDuration       int     `mapstructure:"tortoise-beacon-weak-coin-round-duration-sec"`    // Weak coin round duration in seconds
	Theta                       float64 `mapstructure:"tortoise-beacon-theta"`                           // Ratio of votes for reaching consensus
	VotesLimit                  int     `mapstructure:"tortoise-beacon-votes-limit"`                     // Maximum allowed number of votes to be sent
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		Kappa:                       40,
		Q:                           "1/3",
		RoundsNumber:                300,
		GracePeriodDurationSec:      2 * 60,      // 2 minutes
		ProposalDurationSec:         2 * 60,      // 2 minutes
		FirstVotingRoundDurationSec: 1 * 60 * 60, // 1 hour
		VotingRoundDurationSec:      30 * 60,     // 30 minutes
		WeakCoinRoundDuration:       1 * 60,      // 1 minute
		Theta:                       0.25,
		VotesLimit:                  100, // TODO: around 100, find the calculation in the forum
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		Kappa:                       400000,
		Q:                           "1/3",
		RoundsNumber:                2,
		GracePeriodDurationSec:      1,
		ProposalDurationSec:         1,
		FirstVotingRoundDurationSec: 1,
		VotingRoundDurationSec:      1,
		WeakCoinRoundDuration:       1,
		Theta:                       0.00004,
		VotesLimit:                  100,
	}
}
