package activation

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
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
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	local    p2p.Peer
	logger   *zap.Logger
	versions []atxVersion

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

func WithBonusWeightEpoch(epoch types.EpochID) HandlerOption {
	return func(h *Handler) {
		h.v1.bonusWeightEpoch = epoch
		h.v2.bonusWeightEpoch = epoch
	}
}

// NewHandler returns a data handler for ATX.
func NewHandler(
	local p2p.Peer,
	cdb *datastore.CachedDB,
	atxsdata *atxsdata.Data,
	edVerifier *signing.EdVerifier,
	c layerClock,
	fetcher system.Fetcher,
	goldenATXID types.ATXID,
	nipostValidator nipostValidator,
	malPublisher atxMalfeasancePublisher,
	legacyMalPublisher legacyMalfeasancePublisher,
	beacon atxReceiver,
	tortoise system.Tortoise,
	lg *zap.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		local:    local,
		logger:   lg,
		versions: []atxVersion{{0, types.AtxV1}},

		v1: &HandlerV1{
			local:            local,
			cdb:              cdb,
			atxsdata:         atxsdata,
			edVerifier:       edVerifier,
			clock:            c,
			tickSize:         1,
			bonusWeightEpoch: 0,
			goldenATXID:      goldenATXID,
			nipostValidator:  nipostValidator,
			logger:           lg,
			fetcher:          fetcher,
			beacon:           beacon,
			tortoise:         tortoise,
			malPublisher:     legacyMalPublisher,
			malPublisher2:    malPublisher,
		},

		v2: &HandlerV2{
			local:            local,
			cdb:              cdb,
			atxsdata:         atxsdata,
			edVerifier:       edVerifier,
			clock:            c,
			tickSize:         1,
			bonusWeightEpoch: 0,
			goldenATXID:      goldenATXID,
			nipostValidator:  nipostValidator,
			logger:           lg,
			fetcher:          fetcher,
			beacon:           beacon,
			tortoise:         tortoise,
			malPublisher:     malPublisher,
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
		})),
	)
	return h
}

// HandleSyncedAtx handles atxs received by sync.
func (h *Handler) HandleSyncedAtx(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	err := h.handleAtx(ctx, expHash, peer, data)
	switch {
	case errors.Is(err, errKnownAtx):
		return nil
	case errors.Is(err, errMalformedData):
		h.logger.Debug("malformed atx",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
		)
		return err
	case err != nil:
		h.logger.Warn("failed to process synced atx",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
		)
		return err
	}
	return nil
}

// HandleGossipAtx handles the atx gossip data channel.
func (h *Handler) HandleGossipAtx(ctx context.Context, peer p2p.Peer, msg []byte) error {
	err := h.handleAtx(ctx, types.EmptyHash32, peer, msg)
	switch {
	case errors.Is(err, errKnownAtx) && peer == h.local:
		return nil
	case errors.Is(err, errKnownAtx):
		return errKnownAtx
	case errors.Is(err, errMalformedData):
		h.logger.Debug("malformed atx gossip",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
		)
		return err
	case err != nil:
		h.logger.Warn("failed to process atx gossip",
			log.ZContext(ctx),
			zap.Stringer("sender", peer),
			zap.Error(err),
		)
		return err
	}
	return nil
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

func (h *Handler) handleAtx(ctx context.Context, expHash types.Hash32, peer p2p.Peer, msg []byte) error {
	receivedTime := time.Now()

	opaqueAtx, err := h.decodeATX(msg)
	if err != nil {
		return fmt.Errorf("%w: decoding ATX: %w", pubsub.ErrValidationReject, err)
	}
	id := opaqueAtx.ID()

	if expHash != types.EmptyHash32 && id.Hash32() != expHash {
		return fmt.Errorf("%w: atx want %s, got %s", errWrongHash, expHash.ShortString(), id.ShortString())
	}

	key := string(id.Bytes())
	_, err, _ = h.inProgress.Do(key, func() (any, error) {
		h.logger.Debug("handling incoming atx",
			log.ZContext(ctx),
			zap.Stringer("atx_id", id),
			zap.Int("size", len(msg)),
		)

		switch atx := opaqueAtx.(type) {
		case *wire.ActivationTxV1:
			return nil, h.v1.processATX(ctx, peer, atx, receivedTime)
		case *wire.ActivationTxV2:
			return nil, h.v2.processATX(ctx, peer, atx, receivedTime)
		default:
			panic("unreachable")
		}
	})
	h.inProgress.Forget(key)
	return err
}

func calcWeight(
	numUnits, tickCount uint64,
	rewardBonusEpoch, commitmentEpoch, publishEpoch types.EpochID,
) (uint64, error) {
	hi, weight := bits.Mul64(numUnits, tickCount)
	if hi != 0 {
		return 0, fmt.Errorf("weight overflow (%d * %d)", numUnits, tickCount)
	}
	if rewardBonusEpoch == 0 {
		// no bonus epoch configured
		return weight, nil
	}
	if commitmentEpoch < rewardBonusEpoch-2 {
		// An identity selecting a commitment in epoch X will init in epoch X and create an initial post. Now there are
		// two scenarios:
		// 1. The identity has enough time to register at PoET during the cyclegap of epoch X, and the initial ATX will
		// be published in epoch X+1.
		// 2. The cyclegap already closed in epoch X and the identity will publish the initial ATX in epoch X+2.
		//
		// since 2) is the more common case (most ATXs that could be selected for commitment are published during the
		// cyclegap) we allow a 2 epoch gap between the commitment and the reward bonus epoch.
		return weight, nil
	}
	if publishEpoch < rewardBonusEpoch { // bonus hasn't started yet
		return weight, nil
	}
	epochsSinceBonus := uint64(min(publishEpoch-rewardBonusEpoch+1, 10)) // we scale the bonus over 10 epochs ...
	hi, bonusWeight := bits.Mul64(weight, epochsSinceBonus)
	if hi != 0 {
		return 0, fmt.Errorf("bonus weight overflow (%d * %d)", weight, epochsSinceBonus)
	}
	return weight + (bonusWeight / 10), nil // ... linearly to 100% extra weight
}
