package types

// Votes is encoded votes.
type Votes struct {
	// Base ballot.
	Base BallotID
	// Exceptions is the difference between base ballot votes and local opinion.
	Support, Against []BlockID
	// Abstain on layers until they are terminated.
	Abstain []LayerID
}
