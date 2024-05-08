package activation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

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

type atxVersion struct {
	// epoch since this version is valid
	publish types.EpochID
	types.AtxVersion
}

type AtxVersions map[types.EpochID]types.AtxVersion

func (v AtxVersions) asSlice() []atxVersion {
	var versions []atxVersion
	for epoch, version := range v {
		versions = append(versions, atxVersion{epoch, version})
	}
	slices.SortFunc(versions, func(a, b atxVersion) int { return int(int64(a.publish) - int64(b.publish)) })
	return versions
}

func (v AtxVersions) Validate() error {
	versions := v.asSlice()
	lastVersion := types.AtxV1
	for _, v := range versions {
		if v.AtxVersion < types.AtxV1 || v.AtxVersion > types.AtxVMAX {
			return fmt.Errorf("ATX version: %v not in range [%v:%v]", v, types.AtxV1, types.AtxVMAX)
		}
		if v.AtxVersion < lastVersion {
			return fmt.Errorf("cannot decrease ATX version from %v to %v", lastVersion, v.AtxVersion)
		}
		lastVersion = v.AtxVersion
	}
	return nil
}

// Handler processes the atxs received from all nodes and their validity status.
type Handler struct {
	local     p2p.Peer
	publisher pubsub.Publisher
	log       log.Log
	versions  []atxVersion

	// inProgress map gathers ATXs that are currently being processed.
	// It's used to avoid processing the same ATX twice.
	inProgress   map[types.ATXID][]chan error
	inProgressMu sync.Mutex

	v1 *HandlerV1
}

// HandlerOption is a functional option for the handler.
type HandlerOption func(*Handler)

func WithAtxVersions(v AtxVersions) HandlerOption {
	return func(h *Handler) {
		h.versions = append(h.versions, v.asSlice()...)
	}
}

func WithTickSize(tickSize uint64) HandlerOption {
	return func(h *Handler) {
		h.v1.tickSize = tickSize
	}
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
	goldenATXID types.ATXID,
	nipostValidator nipostValidator,
	beacon AtxReceiver,
	tortoise system.Tortoise,
	lg log.Log,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		local:     local,
		publisher: pub,
		log:       lg,
		versions:  []atxVersion{{0, types.AtxV1}},

		v1: &HandlerV1{
			local:           local,
			cdb:             cdb,
			atxsdata:        atxsdata,
			edVerifier:      edVerifier,
			clock:           c,
			tickSize:        1,
			goldenATXID:     goldenATXID,
			nipostValidator: nipostValidator,
			log:             lg,
			fetcher:         fetcher,
			beacon:          beacon,
			tortoise:        tortoise,
			signers:         make(map[types.NodeID]*signing.EdSigner),
		},

		inProgress: make(map[types.ATXID][]chan error),
	}

	for _, opt := range opts {
		opt(h)
	}

	h.log.With().Info("atx handler created",
		log.Array("supported ATX versions", log.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
			for _, v := range h.versions {
				enc.AppendString(fmt.Sprintf("v%v from epoch %d", v.AtxVersion, v.publish))
			}
			return nil
		})))

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

func (h *Handler) determineVersion(msg []byte) (*types.AtxVersion, error) {
	// The first field of all ATXs is the publish epoch, which
	// we use to determine the version of the ATX.
	var publish types.EpochID
	if err := codec.Decode(msg, &publish); err != nil && !errors.Is(err, codec.ErrShortRead) {
		return nil, fmt.Errorf("%w: %w", errMalformedData, err)
	}

	version := types.AtxV1
	for _, v := range h.versions {
		if publish >= v.publish {
			version = v.AtxVersion
		}
	}
	return &version, nil
}

type opaqueAtx interface {
	ID() types.ATXID
}

func (h *Handler) decodeATX(msg []byte) (opaqueAtx, error) {
	version, err := h.determineVersion(msg)
	if err != nil {
		return nil, fmt.Errorf("determining ATX version: %w", err)
	}

	switch *version {
	case types.AtxV1:
		var atx wire.ActivationTxV1
		if err := codec.Decode(msg, &atx); err != nil {
			return nil, fmt.Errorf("%w: %w", errMalformedData, err)
		}
		return &atx, nil
	}

	return nil, fmt.Errorf("unsupported ATX version: %v", *version)
}

func (h *Handler) handleAtx(
	ctx context.Context,
	expHash types.Hash32,
	peer p2p.Peer,
	msg []byte,
) (*mwire.MalfeasanceProof, error) {
	receivedTime := time.Now()

	opaqueAtx, err := h.decodeATX(msg)
	if err != nil {
		return nil, fmt.Errorf("decoding ATX: %w", err)
	}
	id := opaqueAtx.ID()

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

	var proof *mwire.MalfeasanceProof

	switch atx := opaqueAtx.(type) {
	case *wire.ActivationTxV1:
		proof, err = h.v1.processATX(ctx, peer, atx, msg, receivedTime)
	default:
		panic("unreachable")
	}

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
