package hare3

import "github.com/spacemeshos/go-spacemesh/common/types"

// GradedGossiper works as a filter, for the given message inputs it returns
// one of 3 results indicating what action to take with the message it was
// invoked for.
type GradedGossiper interface {
	// ReceiveMsg accepts a hash identifying the message an id identifying the
	// originator of the message, a hash identifying the value in the message
	// the message round and a grade. It returns a value indicating what action
	// to take with the message and the hash of an equivocating message if
	// equivocation was detected.
	//
	// There are 3 different results that graded gossip can output:
	//
	// * SendValue indicates that the message be forwarded to downstream sub
	// protocols (Gradecaster & TrhesholdGradedGossiper) and ultimately that
	// the message should be relayed to the network. Note that the first
	// message in a round from a known malicious identity needs to be
	// forwarded, since hare3 includes the number of known malicious entities
	// in its threshold calculations. Also it is necessary to forward it so
	// that the rest of the network can also include the malicious identity in
	// their threshold calculations. Messages from known malicious entities are
	// signified to GradedGossiper by having a nil valueHash.
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
		hash,
		id types.Hash20,
		valueHash *types.Hash20,
		msgRound AbsRound,
	) (result GradedGossipResult, equivocationHash *types.Hash20)
}

type ggstate struct {
	hash      types.Hash20
	valueHash *types.Hash20
}

type DefaultGradedGossiper struct {
	// msgs maps message round to sender id to ...
	msgs map[AbsRound]map[types.Hash20]*ggstate
}

// ReceiveMsg implements GradedGossiper.
func (gg *DefaultGradedGossiper) ReceiveMsg(
	hash types.Hash20,
	id types.Hash20,
	valueHash *types.Hash20,
	msgRound AbsRound,
	grade uint8,
) (result GradedGossipResult, equivocationHash *types.Hash20) {
	senderStates := Ensure(gg.msgs, msgRound)
	state := senderStates[id]
	switch {
	case state == nil:
		// If there was no previous message for this identity, store the
		// message and forward it.
		senderStates[id] = &ggstate{
			hash:      hash,
			valueHash: valueHash,
		}
		return SendValue, nil
	case state.valueHash == nil:
		// This indicates that we've already forwarded a message indicating
		// that this entity is malicious, so we drop further messages.
		return DropMessage, nil
	default:
		// In this case we have detected a new equivocation, we return the hash
		// of the previous message so that the caller can generate a proof.
		return SendEquivocationProof, &state.hash
	}
}
