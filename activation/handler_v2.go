package activation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/post/shared"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -source=./handler_v2.go -destination=mocks_handler_v2.go -package=activation

type nipostValidatorV2 interface {
	IsVerifyingFullPost() bool
	VRFNonceV2(smesherID types.NodeID, commitment types.ATXID, vrfNonce types.VRFPostIndex, numUnits uint32) error
	PostV2(
		ctx context.Context,
		smesherID types.NodeID,
		commitment types.ATXID,
		post *wire.PostV1,
		challenge []byte,
		numUnits uint32,
		opts ...validatorOption,
	) error
}

type HandlerV2 struct {
	local           p2p.Peer
	cdb             *datastore.CachedDB
	atxsdata        *atxsdata.Data
	edVerifier      *signing.EdVerifier
	clock           layerClock
	tickSize        uint64
	goldenATXID     types.ATXID
	nipostValidator nipostValidatorV2
	beacon          AtxReceiver
	tortoise        system.Tortoise
	log             *zap.Logger
	fetcher         system.Fetcher
}

func (h *HandlerV2) processATX(
	ctx context.Context,
	peer p2p.Peer,
	watx *wire.ActivationTxV2,
	blob []byte,
	received time.Time,
) (*mwire.MalfeasanceProof, error) {
	exists, err := atxs.Has(h.cdb, watx.ID())
	if err != nil {
		return nil, fmt.Errorf("failed to check if atx exists: %w", err)
	}
	if exists {
		return nil, nil
	}

	h.log.Debug(
		"processing atx",
		log.ZContext(ctx),
		zap.Stringer("atx_id", watx.ID()),
		zap.Uint32("publish", watx.PublishEpoch.Uint32()),
		zap.Stringer("smesherID", watx.SmesherID),
	)

	if err := h.syntacticallyValidate(ctx, watx); err != nil {
		return nil, fmt.Errorf("atx %s syntactically invalid: %w", watx.ID(), err)
	}

	// TODO:
	// 1. fetch dependencies
	// 2. syntactically validate dependencies
	// 3. contextually validate ATX
	// 4. store ATX
	return nil, nil
}

// Syntactically validate an ATX.
// TODOs:
// 1. support marriages
// 2. support merged ATXs.
func (h *HandlerV2) syntacticallyValidate(ctx context.Context, atx *wire.ActivationTxV2) error {
	if !h.edVerifier.Verify(signing.ATX, atx.SmesherID, atx.SignedBytes(), atx.Signature) {
		return fmt.Errorf("invalid atx signature: %w", errMalformedData)
	}
	if atx.PositioningATX == types.EmptyATXID {
		return errors.New("empty positioning atx")
	}
	// TODO: support marriages
	if len(atx.Marriages) != 0 {
		return errors.New("marriages are not supported")
	}

	current := h.clock.CurrentLayer().GetEpoch()
	if atx.PublishEpoch > current+1 {
		return fmt.Errorf("atx publish epoch is too far in the future: %d > %d", atx.PublishEpoch, current+1)
	}

	if atx.MarriageATX == nil {
		if len(atx.NiPosts) != 1 {
			return errors.New("solo atx must have one nipost")
		}
		if len(atx.NiPosts[0].Posts) != 1 {
			return errors.New("solo atx must have one post")
		}
		if atx.NiPosts[0].Posts[0].PrevATXIndex != 0 {
			return errors.New("solo atx post must have prevATXIndex 0")
		}
	}

	if atx.Initial != nil {
		if atx.MarriageATX != nil {
			return errors.New("initial atx cannot reference a marriage atx")
		}
		if atx.Initial.CommitmentATX == types.EmptyATXID {
			return errors.New("initial atx missing commitment atx")
		}
		if atx.VRFNonce == nil {
			return errors.New("initial atx missing vrf nonce")
		}
		if len(atx.PreviousATXs) != 0 {
			return errors.New("initial atx must not have previous atxs")
		}

		if atx.Coinbase == nil {
			return errors.New("initial atx missing coinbase")
		}

		numUnits := atx.NiPosts[0].Posts[0].NumUnits
		if err := h.nipostValidator.VRFNonceV2(
			atx.SmesherID, atx.Initial.CommitmentATX, types.VRFPostIndex(*atx.VRFNonce), numUnits,
		); err != nil {
			return fmt.Errorf("invalid vrf nonce: %w", err)
		}
		if err := h.nipostValidator.PostV2(
			ctx, atx.SmesherID, atx.Initial.CommitmentATX, &atx.Initial.Post, shared.ZeroChallenge, numUnits,
		); err != nil {
			return fmt.Errorf("invalid initial post: %w", err)
		}
		return nil
	}

	for i, prev := range atx.PreviousATXs {
		if prev == types.EmptyATXID {
			return fmt.Errorf("previous atx[%d] is empty", i)
		}
		if prev == h.goldenATXID {
			return fmt.Errorf("previous atx[%d] is the golden ATX", i)
		}
	}

	switch {
	case atx.MarriageATX != nil:
		// Merged ATX
		// TODO: support merged ATXs
		return errors.New("atx merge is not supported")
	default:
		// Solo chained (non-initial) ATX
		if len(atx.PreviousATXs) != 1 {
			return errors.New("solo atx must have one previous atx")
		}
	}

	return nil
}
