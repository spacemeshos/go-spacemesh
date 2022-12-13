package beacon

import (
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Config is the configuration of the beacon.
type Config struct {
	Kappa                    int           `mapstructure:"beacon-kappa"`                       // Security parameter (for calculating ATX threshold)
	Q                        *big.Rat      `mapstructure:"beacon-q"`                           // Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.
	RoundsNumber             types.RoundID `mapstructure:"beacon-rounds-number"`               // Amount of rounds in every epoch
	GracePeriodDuration      time.Duration `mapstructure:"beacon-grace-period-duration"`       // Grace period duration
	ProposalDuration         time.Duration `mapstructure:"beacon-proposal-duration"`           // Proposal phase duration
	FirstVotingRoundDuration time.Duration `mapstructure:"beacon-first-voting-round-duration"` // First voting round duration
	VotingRoundDuration      time.Duration `mapstructure:"beacon-voting-round-duration"`       // Voting round duration
	WeakCoinRoundDuration    time.Duration `mapstructure:"beacon-weak-coin-round-duration"`    // Weak coin round duration
	Theta                    *big.Rat      `mapstructure:"beacon-theta"`                       // Ratio of votes for reaching consensus
	VotesLimit               uint32        `mapstructure:"beacon-votes-limit"`                 // Maximum allowed number of votes to be sent
	BeaconSyncNumBallots     uint32        `mapstructure:"beacon-sync-num-blocks"`             // Numbers of layers to wait before determining beacon values from ballots when the node didn't participate in previous epoch.
}

// DefaultConfig returns the default configuration for the beacon.
func DefaultConfig() Config {
	return Config{
		Kappa:                    40,
		Q:                        big.NewRat(1, 3),
		RoundsNumber:             300,
		GracePeriodDuration:      2 * time.Minute,
		ProposalDuration:         2 * time.Minute,
		FirstVotingRoundDuration: 1 * time.Hour,
		VotingRoundDuration:      30 * time.Minute,
		WeakCoinRoundDuration:    1 * time.Minute,
		Theta:                    big.NewRat(1, 4),
		VotesLimit:               100,  // TODO: around 100, find the calculation in the forum
		BeaconSyncNumBallots:     1600, // should be 2 clusters of 800 ballots
	}
}

// UnitTestConfig returns the unit test configuration for the beacon.
func UnitTestConfig() Config {
	return Config{
		Kappa:                    40,
		Q:                        big.NewRat(1, 3),
		RoundsNumber:             10,
		GracePeriodDuration:      20 * time.Millisecond,
		ProposalDuration:         20 * time.Millisecond,
		FirstVotingRoundDuration: 40 * time.Millisecond,
		VotingRoundDuration:      20 * time.Millisecond,
		WeakCoinRoundDuration:    20 * time.Millisecond,
		Theta:                    big.NewRat(1, 25000),
		VotesLimit:               100,
		BeaconSyncNumBallots:     2,
	}
}

// NodeSimUnitTestConfig returns configuration for the beacon the unit tests with node simulation .
func NodeSimUnitTestConfig() Config {
	return Config{
		Kappa:                    40,
		Q:                        big.NewRat(1, 3),
		RoundsNumber:             2,
		GracePeriodDuration:      200 * time.Millisecond,
		ProposalDuration:         100 * time.Millisecond,
		FirstVotingRoundDuration: 100 * time.Millisecond,
		VotingRoundDuration:      100 * time.Millisecond,
		WeakCoinRoundDuration:    100 * time.Millisecond,
		Theta:                    big.NewRat(1, 25000),
		VotesLimit:               100,
		BeaconSyncNumBallots:     10,
	}
}
