package types

// Votes is encoded votes.
type Votes struct {
	// Base ballot.
	Base BallotID
	// Support and Against blocks that base ballot votes differently.
	Support, Against []BlockID
	// Abstain on layers until they are terminated.
	Abstain []LayerID
}
