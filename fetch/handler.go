package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

// errInternal is returned from the peer when the peer encounters an internal error.
var errInternal = errors.New("unspecified error returned by peer")

type handler struct {
	logger log.Log
	db     *sql.Database
	bs     *datastore.BlobStore
	msh    meshProvider
}

func newHandler(db *sql.Database, bs *datastore.BlobStore, msh meshProvider, lg log.Log) *handler {
	return &handler{
		logger: lg,
		db:     db,
		bs:     bs,
		msh:    msh,
	}
}

// handleEpochATXIDsReq returns the ATXs published in the specified epoch.
func (h *handler) handleEpochATXIDsReq(ctx context.Context, msg []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(msg))
	atxids, err := atxs.GetIDsByEpoch(h.db, epoch)
	if err != nil {
		return nil, fmt.Errorf("get epoch ATXs for epoch %v: %w", epoch, err)
	}

	h.logger.WithContext(ctx).With().Debug("responded to epoch atx request",
		epoch,
		log.Int("count", len(atxids)))
	bts, err := codec.Encode(atxids)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize epoch atx", epoch, log.Err(err))
		return bts, fmt.Errorf("serialize: %w", err)
	}

	return bts, nil
}

// handleLayerDataReq returns the block IDs for the specified layer hash,
// it also returns the validation vector for this data and the latest blocks received in gossip.
func (h *handler) handleLayerDataReq(ctx context.Context, req []byte) ([]byte, error) {
	lyrID := types.BytesToLayerID(req)
	processed := h.msh.ProcessedLayer()
	if lyrID.After(processed) {
		return nil, fmt.Errorf("%w: requested layer %v is higher than processed %v", errLayerNotProcessed, lyrID, processed)
	}
	ld := &layerData{ProcessedLayer: processed}
	var err error
	ld.Hash, err = layers.GetHash(h.db, lyrID)
	if err != nil {
		h.logger.WithContext(ctx).With().Warning("failed to get layer hash", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.AggregatedHash, err = layers.GetAggregatedHash(h.db, lyrID)
	if err != nil {
		h.logger.WithContext(ctx).With().Warning("failed to get aggregated layer hash", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.Ballots, err = ballots.IDsInLayer(h.db, lyrID)
	if err != nil {
		// sqh.ErrNotFound should be considered a programming error since we are only responding for
		// layers older than processed layer
		h.logger.WithContext(ctx).With().Warning("failed to get layer ballots", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.Blocks, err = blocks.IDsInLayer(h.db, lyrID)
	if err != nil {
		// sqh.ErrNotFound should be considered a programming error since we are only responding for
		// layers older than processed layer
		h.logger.WithContext(ctx).With().Warning("failed to get layer blocks", lyrID, log.Err(err))
		return nil, errInternal
	}
	if ld.HareOutput, err = layers.GetHareOutput(h.db, lyrID); err != nil {
		h.logger.WithContext(ctx).With().Warning("failed to get hare output for layer", lyrID, log.Err(err))
		return nil, errInternal
	}

	out, err := codec.Encode(ld)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize layer blocks response", log.Err(err))
	}

	return out, nil
}

func (h *handler) handleHashReq(ctx context.Context, data []byte) ([]byte, error) {
	var requestBatch requestBatch
	err := types.BytesToInterface(data, &requestBatch)
	if err != nil {
		h.logger.WithContext(ctx).With().Error("failed to parse request", log.Err(err))
		return nil, errors.New("bad request")
	}
	resBatch := responseBatch{
		ID:        requestBatch.ID,
		Responses: make([]responseMessage, 0, len(requestBatch.Requests)),
	}
	// this will iterate all requests and populate appropriate Responses, if there are any missing items they will not
	// be included in the response at all
	for _, r := range requestBatch.Requests {
		res, err := h.bs.Get(r.Hint, r.Hash.Bytes())
		if err != nil {
			h.logger.WithContext(ctx).With().Info("remote peer requested nonexistent hash",
				log.String("hash", r.Hash.ShortString()),
				log.String("hint", string(r.Hint)),
				log.Err(err))
			continue
		} else {
			h.logger.WithContext(ctx).With().Debug("responded to hash request",
				log.String("hash", r.Hash.ShortString()),
				log.Int("dataSize", len(res)))
		}
		// add response to batch
		m := responseMessage{
			Hash: r.Hash,
			Data: res,
		}
		resBatch.Responses = append(resBatch.Responses, m)
	}

	bts, err := types.InterfaceToBytes(&resBatch)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize batch id",
			log.String("batch_hash", resBatch.ID.ShortString()))
	}
	h.logger.WithContext(ctx).With().Debug("returning response for batch",
		log.String("batch_hash", resBatch.ID.ShortString()),
		log.Int("count_responses", len(resBatch.Responses)),
		log.Int("data_size", len(bts)))
	return bts, nil
}
