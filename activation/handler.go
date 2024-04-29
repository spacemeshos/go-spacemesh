package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system"
)

var (
	errKnownAtx      = errors.New("known atx")
	errMalformedData = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash     = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errMaliciousATX  = errors.New("malicious atx")
)

// Handler processes the atxs received from all nodes and their validity status.
type Handler struct {
	local     p2p.Peer
	publisher pubsub.Publisher
	log       log.Log

	// inProgress map gathers ATXs that are currently being processed.
	// It's used to avoid processing the same ATX twice.
	inProgress   map[types.ATXID][]chan error
	inProgressMu sync.Mutex

	v1 *HandlerV1
}

// NewHandler returns a data handler for ATX.
func NewHandler(
	local p2p.Peer,
	cdb *datastore.CachedDB,
	atxsdata *atxsdata.Data,
	edVerifier *signing.EdVerifier,
	c layerClock,
	pub pubsub.Publisher,
	fetcher system.Fetcher,
	tickSize uint64,
	goldenATXID types.ATXID,
	nipostValidator nipostValidator,
	beacon AtxReceiver,
	tortoise system.Tortoise,
	log log.Log,
) *Handler {
	h := &Handler{
		local:     local,
		publisher: pub,
		log:       log,
		v1: &HandlerV1{
			local:           local,
			cdb:             cdb,
			atxsdata:        atxsdata,
			edVerifier:      edVerifier,
			clock:           c,
			tickSize:        tickSize,
			goldenATXID:     goldenATXID,
			nipostValidator: nipostValidator,
			log:             log,
			fetcher:         fetcher,
			beacon:          beacon,
			tortoise:        tortoise,
			signers:         make(map[types.NodeID]*signing.EdSigner),
		},

		inProgress: make(map[types.ATXID][]chan error),
	}

	return h
}

func (h *Handler) Register(sig *signing.EdSigner) {
	h.v1.Register(sig)
}

// HandleSyncedAtx handles atxs received by sync.
func (h *Handler) HandleSyncedAtx(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	_, err := h.handleAtx(ctx, expHash, peer, data)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.log.WithContext(ctx).With().Warning("failed to process synced atx",
			log.Stringer("sender", peer),
			log.Err(err),
		)
	}
	if errors.Is(err, errKnownAtx) {
		return nil
	}
	return err
}

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	proof, err := h.handleAtx(ctx, types.Hash32{}, peer, msg)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.log.WithContext(ctx).With().Warning("failed to process atx gossip",
			log.Stringer("sender", peer),
			log.Err(err),
		)
	}
	if errors.Is(err, errKnownAtx) && peer == h.local {
		return nil
	}

	// broadcast malfeasance proof last as the verification of the proof will take place
	// in the same goroutine
	if proof != nil {
		gossip := mwire.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		}
		encodedProof := codec.MustEncode(&gossip)
		if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, encodedProof); err != nil {
			h.log.With().Error("failed to broadcast malfeasance proof", log.Err(err))
			return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
		}
		return errMaliciousATX
	}
	return err
}

func (h *Handler) handleAtx(
	ctx context.Context,
	expHash types.Hash32,
	peer p2p.Peer,
	msg []byte,
) (*mwire.MalfeasanceProof, error) {
	receivedTime := time.Now()

	var atx wire.ActivationTxV1
	if err := codec.Decode(msg, &atx); err != nil {
		return nil, fmt.Errorf("%w: %w", errMalformedData, err)
	}
	id := atx.ID()
	if (expHash != types.Hash32{}) && id.Hash32() != expHash {
		return nil, fmt.Errorf("%w: atx want %s, got %s", errWrongHash, expHash.ShortString(), id.ShortString())
	}

	// Check if processing is already in progress
	h.inProgressMu.Lock()
	if sub, ok := h.inProgress[id]; ok {
		ch := make(chan error, 1)
		h.inProgress[id] = append(sub, ch)
		h.inProgressMu.Unlock()
		h.log.WithContext(ctx).With().Debug("atx is already being processed. waiting for result", id)
		select {
		case err := <-ch:
			h.log.WithContext(ctx).With().Debug("atx processed in other task", id, log.Err(err))
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	h.inProgress[id] = []chan error{}
	h.inProgressMu.Unlock()
	h.log.WithContext(ctx).With().Info("handling incoming atx", id, log.Int("size", len(msg)))

	proof, err := h.v1.processATX(ctx, peer, atx, msg, receivedTime)
	h.inProgressMu.Lock()
	defer h.inProgressMu.Unlock()
	for _, ch := range h.inProgress[id] {
		ch <- err
		close(ch)
	}
	delete(h.inProgress, id)
	return proof, err
}

// Obtain the atxSignature of the given ATX.
func atxSignature(ctx context.Context, db sql.Executor, id types.ATXID) (types.EdSignature, error) {
	var blob sql.Blob
	if err := atxs.LoadBlob(ctx, db, id.Bytes(), &blob); err != nil {
		return types.EmptyEdSignature, err
	}

	if blob.Bytes == nil {
		// An empty blob indicates a golden ATX (after a checkpoint-recovery).
		return types.EmptyEdSignature, fmt.Errorf("can't get signature for a golden (checkpointed) ATX: %s", id)
	}

	// TODO: decide how to decode based on the `version` column.
	var prev wire.ActivationTxV1
	if err := codec.Decode(blob.Bytes, &prev); err != nil {
		return types.EmptyEdSignature, fmt.Errorf("decoding previous atx: %w", err)
	}
	return prev.Signature, nil
}
