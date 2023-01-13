package malfeasance

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

var errMalformedData = errors.New("malformed data")

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger log.Log
	db     *sql.Database
	self   p2p.Peer
}

func NewHandler(db *sql.Database, lg log.Log, self p2p.Peer) *Handler {
	return &Handler{logger: lg, db: db, self: self}
}

// HandleMalfeasanceProof is the gossip receiver for MalfeasanceProof.
func (h *Handler) HandleMalfeasanceProof(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	err := h.handleProof(ctx, peer, msg)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedData):
		return pubsub.ValidationReject
	default:
		return pubsub.ValidationIgnore
	}
}

func (h *Handler) handleProof(ctx context.Context, peer p2p.Peer, data []byte) error {
	var (
		p          types.MalfeasanceGossip
		nodeID     types.NodeID
		malicious  bool
		proofBytes []byte
		err        error
	)
	if err = codec.Decode(data, &p); err != nil {
		h.logger.WithContext(ctx).With().Error("malformed message", log.Err(err))
		return errMalformedData
	}
	switch p.Proof.Type {
	case types.HareEquivocation:
		nodeID, err = validateHareEquivocation(h.logger.WithContext(ctx), &p.MalfeasanceProof)
	case types.MultipleATXs:
		nodeID, err = validateMultipleATXs(h.logger.WithContext(ctx), &p.MalfeasanceProof)
	case types.MultipleBallots:
		nodeID, err = validateMultipleBallots(h.logger.WithContext(ctx), &p.MalfeasanceProof)
	default:
		return errors.New("unknown malfeasance type")
	}

	updateMetrics(p.Proof)
	if err == nil {
		// TODO: pass on to hare if it's types.HareEquivocation
		if malicious, err = identities.IsMalicious(h.db, nodeID); err != nil {
			return err
		} else if malicious {
			if peer == h.self {
				// node saves malfeasance proof eagerly/atomically with the malicious data.
				return nil
			}
			h.logger.WithContext(ctx).With().Debug("known malicious identity", nodeID)
			return errors.New("known proof")
		}
		if proofBytes, err = codec.Encode(&p.MalfeasanceProof); err != nil {
			h.logger.WithContext(ctx).With().Fatal("failed to encode MalfeasanceProof", log.Err(err))
		}
		if err = identities.SetMalicious(h.db, nodeID, proofBytes); err != nil {
			h.logger.WithContext(ctx).With().Error("failed to save MalfeasanceProof", log.Err(err))
		}
	}
	return err
}

func updateMetrics(tp types.Proof) {
	switch tp.Type {
	case types.HareEquivocation:
		numProofsHare.Inc()
	case types.MultipleATXs:
		numProofsATX.Inc()
	case types.MultipleBallots:
		numProofsBallot.Inc()
	}
}

func validateHareEquivocation(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.HareEquivocation {
		return types.NodeID{}, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.HareEquivocation, proof.Proof.Type)
	}
	var (
		firstNid, nid types.NodeID
		firstMsg, msg types.HareProofMsg
		err           error
	)
	hp, ok := proof.Proof.Data.(*types.HareProof)
	if !ok {
		return types.NodeID{}, errors.New("wrong message type for hare equivocation")
	}
	for _, msg = range hp.Messages {
		nid, err = types.ExtractNodeIDFromSig(msg.SignedBytes(), msg.Signature)
		if err != nil {
			return types.NodeID{}, err
		}
		if firstNid == types.EmptyNodeID {
			firstNid = nid
			firstMsg = msg
		} else if nid == firstNid {
			if msg.InnerMsg.Layer == firstMsg.InnerMsg.Layer &&
				msg.InnerMsg.Round == firstMsg.InnerMsg.Round &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return nid, nil
			}
		}
	}
	logger.With().Warning("received invalid malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("layer_1", firstMsg.InnerMsg.Layer),
		log.Stringer("layer_2", msg.InnerMsg.Layer),
		log.Uint32("round_1", firstMsg.InnerMsg.Round),
		log.Uint32("round_2", msg.InnerMsg.Round),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsHare.Inc()
	return types.NodeID{}, errors.New("invalid malfeasance proof")
}

func validateMultipleATXs(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleATXs {
		return types.NodeID{}, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleATXs, proof.Proof.Type)
	}
	var (
		firstNid, nid types.NodeID
		firstMsg, msg types.AtxProofMsg
		err           error
	)
	ap, ok := proof.Proof.Data.(*types.AtxProof)
	if !ok {
		return types.NodeID{}, errors.New("wrong message type for multiple ATXs")
	}
	for _, msg = range ap.Messages {
		nid, err = types.ExtractNodeIDFromSig(msg.SignedBytes(), msg.Signature)
		if err != nil {
			return types.NodeID{}, err
		}
		if firstNid == types.EmptyNodeID {
			firstNid = nid
			firstMsg = msg
		} else if nid == firstNid {
			if msg.InnerMsg.Target == firstMsg.InnerMsg.Target &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return nid, nil
			}
		}
	}
	logger.With().Warning("received invalid malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("epoch_1", firstMsg.InnerMsg.Target),
		log.Stringer("epoch_2", msg.InnerMsg.Target),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsATX.Inc()
	return types.NodeID{}, errors.New("invalid malfeasance proof")
}

func validateMultipleBallots(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleBallots {
		return types.NodeID{}, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleBallots, proof.Proof.Type)
	}
	var (
		firstNid, nid types.NodeID
		firstMsg, msg types.BallotProofMsg
		err           error
	)
	bp, ok := proof.Proof.Data.(*types.BallotProof)
	if !ok {
		return types.NodeID{}, errors.New("wrong message type for multi ballots")
	}
	for _, msg = range bp.Messages {
		nid, err = types.ExtractNodeIDFromSig(msg.SignedBytes(), msg.Signature)
		if err != nil {
			return types.NodeID{}, err
		}
		if firstNid == types.EmptyNodeID {
			firstNid = nid
			firstMsg = msg
		} else if nid == firstNid {
			if msg.InnerMsg.Layer == firstMsg.InnerMsg.Layer &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return nid, nil
			}
		}
	}
	logger.With().Warning("received invalid malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("layer_1", firstMsg.InnerMsg.Layer),
		log.Stringer("layer_2", msg.InnerMsg.Layer),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsBallot.Inc()
	return types.NodeID{}, errors.New("invalid malfeasance proof")
}
