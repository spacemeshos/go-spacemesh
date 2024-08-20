package types

// RoundID is the round ID used to run any protocol that requires multiple rounds.
type RoundID uint32

const (
	// FirstRound is convenient for initializing the index in a loop.
	FirstRound = RoundID(0)
)
