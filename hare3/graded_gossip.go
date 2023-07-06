package hare3

import "github.com/spacemeshos/go-spacemesh/common/types"

// GradedGossiper works as a filter, for the given message inputs it returns
// one of 3 results indicating what action to take with the message it was
// invoked for.
type GradedGossiper interface {
	// ReceiveMsg accepts a hash identifying the message an id identifying the
	// originator of the message, the values of the message and the message
	// round. It returns a value indicating what action to take with the
	// message and the hash of an equivocating message if equivocation was
	// detected. Passing nil values indicates that the message is from a known
	// malicious identity.
	//
	// There are 3 different results that graded gossip can output:
	//
	// * SendValue indicates that the message be forwarded to downstream sub
	// protocols (Gradecaster & ThresholdGradedGossiper) and ultimately that
	// the message should be relayed to the network. Note that the first
	// message in a round from a known malicious identity needs to be
	// forwarded, since hare3 includes the number of known malicious entities
	// in its threshold calculations. Also it is necessary to forward it so
	// that the rest of the network can also include the malicious identity in
	// their threshold calculations.
	//
	// * SendEquivocationProof is returned in the case that a new equivocation
	// has been detected, the equivocationHash will be the hash of the prior
	// message from the same identity. In this case again the equivocating
	// message still needs to be forwarded, for the same reasons as above.
	//
	// * DropMessage is returned in the case of any further messages from
	// entities for which we have already forwarded a malicious message this
	// round. In this case the message should be ignored and not relayed to the
	// network.
	//
	// Note that there can be at most 2 messages forwarded per identity per
	// round, either:
	//
	// * One good message.
	// * One malicious message from a known eqiuvotator.
	// * One good message followed by a malicious message indicating equivocation.
	ReceiveMsg(
		hash types.Hash20,
		id types.NodeID,
		values []types.Hash20,
		msgRound AbsRound,
	) (result GradedGossipResult, equivocationHash *types.Hash20)
}

// ggstate simply stores the hash of the message, for use in constructing
// equivocation proofs and the values associated with the message.
type ggstate struct {
	hash   types.Hash20
	values []types.Hash20
}

type DefaultGradedGossiper struct {
	// msgs maps message round to sender id to the graded gossiper state.
	msgs map[AbsRound]map[types.NodeID]*ggstate
}

func NewDefaultGradedGossiper() *DefaultGradedGossiper {
	return &DefaultGradedGossiper{msgs: make(map[AbsRound]map[types.NodeID]*ggstate)}
}

// ReceiveMsg implements GradedGossiper.
func (gg *DefaultGradedGossiper) ReceiveMsg(
	hash types.Hash20,
	id types.NodeID,
	values []types.Hash20,
	msgRound AbsRound,
) (result GradedGossipResult, equivocationHash *types.Hash20) {
	senderStates := Ensure(gg.msgs, msgRound)
	state := senderStates[id]
	switch {
	case state == nil:
		// If there was no previous message for this identity, store the
		// message and forward it.
		senderStates[id] = &ggstate{
			hash:   hash,
			values: values,
		}
		return SendValue, nil
	case state.values == nil:
		// Nil values indicates that we've already forwarded a message
		// indicating that this entity is malicious, so we drop further
		// messages.
		return DropMessage, nil
	default:
		// In this case we have detected a new equivocation, we mark the sender
		// as malicious and return the hash of the previous message so that the caller
		// can generate an equivocation proof.
		state.values = nil
		return SendEquivocationProof, &state.hash
	}
}
