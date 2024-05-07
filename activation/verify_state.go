package activation

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	awire "github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func CheckPrevATXs(ctx context.Context, logger *zap.Logger, db sql.Executor) error {
	collisions, err := atxs.PrevATXCollisions(db)
	if err != nil {
		return fmt.Errorf("get prev ATX collisions: %w", err)
	}

	logger.Info("found ATX collisions", zap.Int("count", len(collisions)))
	count := 0
	for _, collision := range collisions {
		select {
		case <-ctx.Done():
			// stop on context cancellation
			return ctx.Err()
		default:
		}

		if collision.NodeID1 != collision.NodeID2 {
			logger.Panic(
				"unexpected collision",
				log.ZShortStringer("NodeID1", collision.NodeID1),
				log.ZShortStringer("NodeID2", collision.NodeID2),
				log.ZShortStringer("ATX1", collision.ATX1),
				log.ZShortStringer("ATX2", collision.ATX2),
			)
		}

		malicious, err := identities.IsMalicious(db, collision.NodeID1)
		if err != nil {
			return fmt.Errorf("get malicious status: %w", err)
		}

		if malicious {
			// already malicious no need to generate proof
			continue
		}

		var blob sql.Blob
		var atx1 awire.ActivationTxV1
		if err := atxs.LoadBlob(ctx, db, collision.ATX1.Bytes(), &blob); err != nil {
			return fmt.Errorf("get blob %s: %w", collision.ATX1.ShortString(), err)
		}
		codec.MustDecode(blob.Bytes, &atx1)

		var atx2 awire.ActivationTxV1
		if err := atxs.LoadBlob(ctx, db, collision.ATX2.Bytes(), &blob); err != nil {
			return fmt.Errorf("get blob %s: %w", collision.ATX2.ShortString(), err)
		}
		codec.MustDecode(blob.Bytes, &atx2)

		proof := &wire.MalfeasanceProof{
			Layer: atx1.Publish.FirstLayer(),
			Proof: wire.Proof{
				Type: wire.InvalidPrevATX,
				Data: &wire.InvalidPrevATXProof{
					Atx1: atx1,
					Atx2: atx2,
				},
			},
		}

		encodedProof := codec.MustEncode(proof)
		if err := identities.SetMalicious(db, collision.NodeID1, encodedProof, time.Now()); err != nil {
			return fmt.Errorf("add malfeasance proof: %w", err)
		}

		count++
	}
	logger.Info("created malfeasance proofs", zap.Int("count", count))
	return nil
}
