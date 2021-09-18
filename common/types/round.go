package types

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

// RoundID is the round ID used to run any protocol that requires multiple rounds.
type RoundID uint32

const (
	// FirstRound is convenient for initializing the index in a loop
	FirstRound = RoundID(0)
)

// Field returns a log field. Implements the LoggableField interface.
func (r RoundID) Field() log.Field {
	return log.Uint32("round_id", uint32(r))
}
