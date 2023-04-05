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
	"github.com/spacemeshos/go-spacemesh/signing"
)

var errMalformedData = errors.New("malformed data")

// Handler processes MalfeasanceProof from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger          log.Log
	cdb             *datastore.CachedDB
	self            p2p.Peer
	cp              consensusProtocol
	pubKeyExtractor *signing.PubKeyExtractor
	edVerifier      *signing.EdVerifier
}

func NewHandler(
	cdb *datastore.CachedDB,
	lg log.Log,
	self p2p.Peer,
	cp consensusProtocol,
	pubKeyExtractor *signing.PubKeyExtractor,
	edVerifier *signing.EdVerifier,
) *Handler {
	return &Handler{
		logger:          lg,
		cdb:             cdb,
		self:            self,
		cp:              cp,
		pubKeyExtractor: pubKeyExtractor,
		edVerifier:      edVerifier,
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

func (h *Handler) HandleSyncedMalfeasanceProof(ctx context.Context, peer p2p.Peer, msg []byte) error {
	return h.handleProof(ctx, peer, msg)
}

func (h *Handler) handleProof(ctx context.Context, peer p2p.Peer, data []byte) error {
	var (
		p         types.MalfeasanceGossip
		nodeID    types.NodeID
		malicious bool
		err       error
	)
	logger := h.logger.WithContext(ctx)
	if err = codec.Decode(data, &p); err != nil {
		logger.With().Error("malformed message", log.Err(err))
		return errMalformedData
	}
	switch p.Proof.Type {
	case types.HareEquivocation:
		nodeID, err = h.validateHareEquivocation(logger, &p.MalfeasanceProof)
	case types.MultipleATXs:
		nodeID, err = h.validateMultipleATXs(logger, &p.MalfeasanceProof)
	case types.MultipleBallots:
		nodeID, err = h.validateMultipleBallots(logger, &p.MalfeasanceProof)
	default:
		return errors.New("unknown malfeasance type")
	}

	if err != nil {
		h.logger.WithContext(ctx).With().Warning("failed to validate malfeasance proof",
			log.Inline(&p),
			log.Err(err),
		)
		return err
	}

	// msg is valid
	if p.Eligibility != nil {
		if err = h.validateHareEligibility(ctx, logger, nodeID, &p); err != nil {
			h.logger.WithContext(ctx).With().Warning("failed to validate hare eligibility",
				log.Stringer("smesher", nodeID),
				log.Inline(&p),
				log.Err(err),
			)
			return err
		}
	}

	if malicious, err = h.cdb.IsMalicious(nodeID); err != nil {
		return fmt.Errorf("check known malicious: %w", err)
	} else if malicious {
		if peer == h.self {
			// node saves malfeasance proof eagerly/atomically with the malicious data.
			updateMetrics(p.Proof)
			return nil
		}
		logger.With().Debug("known malicious identity",
			log.Stringer("smesher", nodeID),
		)
		return errors.New("known proof")
	}
	if err = h.cdb.AddMalfeasanceProof(nodeID, &p.MalfeasanceProof, nil); err != nil {
		h.logger.WithContext(ctx).With().Error("failed to save MalfeasanceProof",
			log.Stringer("smesher", nodeID),
			log.Inline(&p),
			log.Err(err),
		)
		return fmt.Errorf("add malfeasance proof: %w", err)
	}
	updateMetrics(p.Proof)
	h.logger.WithContext(ctx).With().Info("new malfeasance proof",
		log.Stringer("smesher", nodeID),
		log.Inline(&p),
	)
	return nil
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
	exists, err := cdb.IdentityExists(nodeID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("identity does not exist")
	}
	return nil
}

func (h *Handler) validateHareEligibility(ctx context.Context, logger log.Log, nodeID types.NodeID, gossip *types.MalfeasanceGossip) error {
	if gossip == nil || gossip.Eligibility == nil {
		logger.Fatal("invalid input")
	}

	emsg := gossip.Eligibility
	if nodeID != emsg.NodeID {
		return fmt.Errorf("mismatch node id")
	}
	// any type of MalfeasanceProof can be accompanied by a hare eligibility
	// forward the eligibility to hare for the running consensus processes.
	h.cp.HandleEligibility(ctx, emsg)
	return nil
}

func (h *Handler) validateHareEquivocation(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.HareEquivocation {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.HareEquivocation, proof.Proof.Type)
	}
	var (
		firstNid, nid types.NodeID
		firstMsg, msg types.HareProofMsg
		err           error
	)
	hp, ok := proof.Proof.Data.(*types.HareProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for hare equivocation")
	}
	for _, msg = range hp.Messages {
		nid, err = h.pubKeyExtractor.ExtractNodeID(signing.HARE, msg.SignedBytes(), msg.Signature)
		if err != nil {
			return types.EmptyNodeID, err
		}
		if err = checkIdentityExists(h.cdb, nid); err != nil {
			return types.EmptyNodeID, fmt.Errorf("check identity in hare malfeasance %v: %w", nid, err)
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
		log.Stringer("smesher", firstNid),
		log.Stringer("smesher", nid),
		log.Object("first", &firstMsg.InnerMsg),
		log.Object("second", &msg.InnerMsg),
	)
	numInvalidProofsHare.Inc()
	return types.EmptyNodeID, errors.New("invalid hare malfeasance proof")
}

func (h *Handler) validateMultipleATXs(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleATXs {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleATXs, proof.Proof.Type)
	}
	var (
		firstNid types.NodeID
		firstMsg types.AtxProofMsg
	)
	ap, ok := proof.Proof.Data.(*types.AtxProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multiple ATXs")
	}
	for _, msg := range ap.Messages {
		if !h.edVerifier.Verify(signing.ATX, msg.SmesherID, msg.SignedBytes(), msg.Signature) {
			return types.EmptyNodeID, errors.New("invalid signature")
		}
		if err := checkIdentityExists(h.cdb, msg.SmesherID); err != nil {
			return types.EmptyNodeID, fmt.Errorf("check identity in atx malfeasance %v: %w", msg.SmesherID, err)
		}
		if firstNid == types.EmptyNodeID {
			firstNid = msg.SmesherID
			firstMsg = msg
		} else if msg.SmesherID == firstNid {
			if msg.InnerMsg.Target == firstMsg.InnerMsg.Target &&
				msg.InnerMsg.MsgHash != firstMsg.InnerMsg.MsgHash {
				return msg.SmesherID, nil
			}
		}
	}
	logger.With().Warning("received invalid atx malfeasance proof",
		log.Stringer("smesher", ap.Messages[0].SmesherID),
		log.Stringer("smesher", ap.Messages[1].SmesherID),
		log.Object("first", &ap.Messages[0].InnerMsg),
		log.Object("second", &ap.Messages[1].InnerMsg),
	)
	numInvalidProofsATX.Inc()
	return types.EmptyNodeID, errors.New("invalid atx malfeasance proof")
}

func (h *Handler) validateMultipleBallots(logger log.Log, proof *types.MalfeasanceProof) (types.NodeID, error) {
	if proof.Proof.Type != types.MultipleBallots {
		return types.EmptyNodeID, fmt.Errorf("wrong malfeasance type. want %v, got %v", types.MultipleBallots, proof.Proof.Type)
	}
	var (
		firstNid, nid types.NodeID
		firstMsg, msg types.BallotProofMsg
		err           error
	)
	bp, ok := proof.Proof.Data.(*types.BallotProof)
	if !ok {
		return types.EmptyNodeID, errors.New("wrong message type for multi ballots")
	}
	for _, msg = range bp.Messages {
		nid, err = h.pubKeyExtractor.ExtractNodeID(signing.BALLOT, msg.SignedBytes(), msg.Signature)
		if err != nil {
			return types.EmptyNodeID, err
		}
		if err = checkIdentityExists(h.cdb, nid); err != nil {
			return types.EmptyNodeID, fmt.Errorf("check identity in ballot malfeasance %v: %w", nid, err)
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
		log.Stringer("smesher", firstNid),
		log.Stringer("smesher", nid),
		log.Object("first", &firstMsg.InnerMsg),
		log.Object("second", &msg.InnerMsg),
	)
	numInvalidProofsBallot.Inc()
	return types.EmptyNodeID, errors.New("invalid ballot malfeasance proof")
}
