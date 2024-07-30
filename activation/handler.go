package activation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/singleflight"

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
	logger    *zap.Logger
	versions  []atxVersion

	// inProgress is used to avoid processing the same ATX multiple times in parallel.
	inProgress singleflight.Group

	v1 *HandlerV1
	v2 *HandlerV2
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
		h.v2.tickSize = tickSize
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
	lg *zap.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		local:     local,
		publisher: pub,
		logger:    lg,
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
			logger:          lg,
			fetcher:         fetcher,
			beacon:          beacon,
			tortoise:        tortoise,
			signers:         make(map[types.NodeID]*signing.EdSigner),
		},

		v2: &HandlerV2{
			local:           local,
			cdb:             cdb,
			atxsdata:        atxsdata,
			edVerifier:      edVerifier,
			clock:           c,
			tickSize:        1,
			goldenATXID:     goldenATXID,
			nipostValidator: nipostValidator,
			logger:          lg,
			fetcher:         fetcher,
			beacon:          beacon,
			tortoise:        tortoise,
			malPublisher:    nil, // TODO(mafa): use proper implementation
		},
	}

	for _, opt := range opts {
		opt(h)
	}

	h.logger.Info("atx handler created",
		zap.Array("supported ATX versions", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
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
		h.logger.Warn("failed to process synced atx",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
		)
	}
	if errors.Is(err, errKnownAtx) {
		return nil
	}
	return err
}

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	proof, err := h.handleAtx(ctx, types.EmptyHash32, peer, msg)
	if err != nil && !errors.Is(err, errMalformedData) && !errors.Is(err, errKnownAtx) {
		h.logger.Warn("failed to process atx gossip",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
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
			h.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
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

func (h *Handler) decodeATX(msg []byte) (atx opaqueAtx, err error) {
	version, err := h.determineVersion(msg)
	if err != nil {
		return nil, fmt.Errorf("determining ATX version: %w", err)
	}

	switch *version {
	case types.AtxV1:
		atx, err = wire.DecodeAtxV1(msg)
	case types.AtxV2:
		atx, err = wire.DecodeAtxV2(msg)
	default:
		return nil, fmt.Errorf("unsupported ATX version: %v", *version)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errMalformedData, err)
	}
	return atx, nil
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
		return nil, fmt.Errorf("%w: decoding ATX: %w", pubsub.ErrValidationReject, err)
	}
	id := opaqueAtx.ID()

	if (expHash != types.Hash32{}) && id.Hash32() != expHash {
		return nil, fmt.Errorf("%w: atx want %s, got %s", errWrongHash, expHash.ShortString(), id.ShortString())
	}

	key := string(id.Bytes())
	proof, err, _ := h.inProgress.Do(key, func() (any, error) {
		h.logger.Debug("handling incoming atx",
			log.ZContext(ctx),
			zap.Stringer("atx_id", id),
			zap.Int("size", len(msg)),
		)

		switch atx := opaqueAtx.(type) {
		case *wire.ActivationTxV1:
			return h.v1.processATX(ctx, peer, atx, receivedTime)
		case *wire.ActivationTxV2:
			return (*mwire.MalfeasanceProof)(nil), h.v2.processATX(ctx, peer, atx, receivedTime)
		default:
			panic("unreachable")
		}
	})
	h.inProgress.Forget(key)

	return proof.(*mwire.MalfeasanceProof), err
}

// Obtain the atxSignature of the given ATX.
func atxSignature(ctx context.Context, db sql.Executor, id types.ATXID) (types.EdSignature, error) {
	var blob sql.Blob
	v, err := atxs.LoadBlob(ctx, db, id.Bytes(), &blob)
	if err != nil {
		return types.EmptyEdSignature, err
	}

	if len(blob.Bytes) == 0 {
		// An empty blob indicates a golden ATX (after a checkpoint-recovery).
		return types.EmptyEdSignature, fmt.Errorf("can't get signature for a golden (checkpointed) ATX: %s", id)
	}

	// TODO: implement for ATX V2
	switch v {
	case types.AtxV1:
		var atx wire.ActivationTxV1
		if err := codec.Decode(blob.Bytes, &atx); err != nil {
			return types.EmptyEdSignature, fmt.Errorf("decoding atx v1: %w", err)
		}
		return atx.Signature, nil
	}
	return types.EmptyEdSignature, fmt.Errorf("unsupported ATX version: %v", v)
}
