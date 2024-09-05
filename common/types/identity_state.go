package types

import (
	"errors"
	"fmt"
)

var ErrIdentityStateUnknown = errors.New("identity state is unknown")

type IdentityState int

const (
	IdentityStateNotSet IdentityState = iota
	IdentityStateWaitForATXSyncing
	IdentityStateWaitForPoetRoundStart
	IdentityStateWaitForPoetRoundEnd
	IdentityStateFetchingProofs
	IdentityStatePostProving
)

func (s IdentityState) String() string {
	switch s {
	case IdentityStateWaitForATXSyncing:
		return "wait_for_atx_syncing"
	case IdentityStateWaitForPoetRoundStart:
		return "wait_for_poet_round_start"
	case IdentityStateWaitForPoetRoundEnd:
		return "wait_for_poet_round_end"
	case IdentityStateFetchingProofs:
		return "fetching_proofs"
	case IdentityStatePostProving:
		return "post_proving"
	case IdentityStateNotSet:
		return "not_set"
	default:
		panic(fmt.Sprintf(ErrIdentityStateUnknown.Error()+" %d", s))
	}
}
