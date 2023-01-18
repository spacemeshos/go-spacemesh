package malfeasance

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

var errMalformedData = errors.New("malformed data")

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger        log.Log
	cdb           *datastore.CachedDB
	self          p2p.Peer
	committeeSize int
	validator     eligibilityValidator
	relayCh       chan<- *types.HareEligibilityGossip
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg log.Log,
	self p2p.Peer,
	committeeSize int,
	ev eligibilityValidator,
	ch chan<- *types.HareEligibilityGossip,
) *Handler {
	return &Handler{
		logger:        lg,
		cdb:           cdb,
		self:          self,
		committeeSize: committeeSize,
		validator:     ev,
		relayCh:       ch,
	}
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
		p         types.MalfeasanceGossip
		nodeID    types.NodeID
		malicious bool
		err       error
	)
	if err = codec.Decode(data, &p); err != nil {
		h.logger.WithContext(ctx).With().Error("malformed message", log.Err(err))
		return errMalformedData
	}
	logger := h.logger.WithContext(ctx)
	switch p.Proof.Type {
	case types.HareEquivocation:
		nodeID, err = validateHareEquivocation(logger, h.cdb, &p.MalfeasanceProof)
	case types.MultipleATXs:
		nodeID, err = validateMultipleATXs(logger, h.cdb, &p.MalfeasanceProof)
	case types.MultipleBallots:
		nodeID, err = validateMultipleBallots(logger, h.cdb, &p.MalfeasanceProof)
	default:
		return errors.New("unknown malfeasance type")
	}

	if err == nil && p.Eligibility != nil {
		err = h.validateHareEligibility(ctx, nodeID, &p)
	}

	// msg is valid
	if err == nil {
		updateMetrics(p.Proof)
		if malicious, err = h.cdb.IsMalicious(nodeID); err != nil {
			return fmt.Errorf("check known malicious: %w", err)
		} else if malicious {
			if peer == h.self {
				// node saves malfeasance proof eagerly/atomically with the malicious data.
				return nil
			}
			logger.With().Debug("known malicious identity", nodeID)
			return errors.New("known proof")
		}
		if err = h.cdb.AddMalfeasanceProof(nodeID, &p.MalfeasanceProof, nil); err != nil {
			h.logger.WithContext(ctx).With().Error("failed to save MalfeasanceProof", log.Err(err))
		}
	} else {
		h.logger.WithContext(ctx).With().Debug("failed to validate MalfeasanceProof", log.Err(err))
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

func checkIdentityExists(cdb *datastore.CachedDB, nodeID types.NodeID) error {
	if _, err := cdb.GetPrevAtx(nodeID); err != nil {
		return err
	}
	return nil
}

func (h *Handler) validateHareEligibility(ctx context.Context, nodeID types.NodeID, gossip *types.MalfeasanceGossip) error {
	logger := h.logger.WithContext(ctx).WithFields(nodeID)
	if gossip == nil || gossip.Eligibility == nil {
		logger.Fatal("invalid input")
	}

	emsg := gossip.Eligibility
	eNodeID := types.BytesToNodeID(emsg.PubKey)
	if nodeID != eNodeID {
		logger.With().Warning("mismatch node id", log.Stringer("eligibility", eNodeID))
		return fmt.Errorf("mismatch node id")
	}
	eligible, err := h.validator.Validate(ctx, emsg.Layer, emsg.Round, h.committeeSize, nodeID, emsg.Eligibility.Proof, emsg.Eligibility.Count)
	if err != nil {
		logger.With().Warning("failed to validate eligibility", emsg.Layer, log.Uint32("round", emsg.Round))
		return fmt.Errorf("validate eligibility: %w", err)
	}
	if !eligible {
		logger.With().Debug("identity not eligible", emsg.Layer, log.Uint32("round", emsg.Round))
		return fmt.Errorf("identity not eligible %v", nodeID)
	}

	// any type of MalfeasanceProof can be accompanied by a hare eligibility
	// forward the eligibility to hare for the running consensus processes.
	select {
	case <-ctx.Done():
	case h.relayCh <- emsg:
	}

	return nil
}

func validateHareEquivocation(logger log.Log, cdb *datastore.CachedDB, proof *types.MalfeasanceProof) (types.NodeID, error) {
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
		if err = checkIdentityExists(cdb, nid); err != nil {
			return types.NodeID{}, fmt.Errorf("identity in hare malfeasance doesn't exist: %v", nid)
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
	logger.With().Warning("received invalid hare malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("layer_1", firstMsg.InnerMsg.Layer),
		log.Stringer("layer_2", msg.InnerMsg.Layer),
		log.Uint32("round_1", firstMsg.InnerMsg.Round),
		log.Uint32("round_2", msg.InnerMsg.Round),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsHare.Inc()
	return types.NodeID{}, errors.New("invalid hare malfeasance proof")
}

func validateMultipleATXs(logger log.Log, cdb *datastore.CachedDB, proof *types.MalfeasanceProof) (types.NodeID, error) {
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
		if err = checkIdentityExists(cdb, nid); err != nil {
			return types.NodeID{}, fmt.Errorf("identity in hare malfeasance doesn't exist: %v", nid)
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
	logger.With().Warning("received invalid atx malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("epoch_1", firstMsg.InnerMsg.Target),
		log.Stringer("epoch_2", msg.InnerMsg.Target),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsATX.Inc()
	return types.NodeID{}, errors.New("invalid atx malfeasance proof")
}

func validateMultipleBallots(logger log.Log, cdb *datastore.CachedDB, proof *types.MalfeasanceProof) (types.NodeID, error) {
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
		if err = checkIdentityExists(cdb, nid); err != nil {
			return types.NodeID{}, fmt.Errorf("identity in ballot malfeasance doesn't exist: %v", nid)
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
	logger.With().Warning("received invalid ballot malfeasance proof",
		log.Stringer("nodeID_1", firstNid),
		log.Stringer("nodeID_2", nid),
		log.Stringer("layer_1", firstMsg.InnerMsg.Layer),
		log.Stringer("layer_2", msg.InnerMsg.Layer),
		log.Stringer("msg_hash_1", firstMsg.InnerMsg.MsgHash),
		log.Stringer("msg_hash_2", msg.InnerMsg.MsgHash))
	numInvalidProofsBallot.Inc()
	return types.NodeID{}, errors.New("invalid ballot malfeasance proof")
}
