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
}

func newHandler(db *sql.Database, bs *datastore.BlobStore, lg log.Log) *handler {
	return &handler{
		logger: lg,
		db:     db,
		bs:     bs,
	}
}

// handleEpochATXIDsReq returns the ATXs published in the specified epoch.
func (h *handler) handleEpochATXIDsReq(ctx context.Context, msg []byte) ([]byte, error) {
	epoch := types.EpochID(util.BytesToUint32(msg))
	atxids, err := atxs.IDsByEpoch(h.db, epoch)
	if err != nil {
		return nil, fmt.Errorf("get epoch ATXs for epoch %v: %w", epoch, err)
	}

	h.logger.WithContext(ctx).With().Debug("responded to epoch atx request",
		epoch,
		log.Int("count", len(atxids)))
	bts, err := codec.EncodeSlice(atxids)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize epoch atx", epoch, log.Err(err))
		return bts, fmt.Errorf("serialize: %w", err)
	}

	return bts, nil
}

// handleLayerDataReq returns all data in a layer, described in LayerData.
func (h *handler) handleLayerDataReq(ctx context.Context, req []byte) ([]byte, error) {
	var (
		lyrID = types.BytesToLayerID(req)
		ld    LayerData
		err   error
	)
	ld.Hash, err = layers.GetHash(h.db, lyrID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		h.logger.WithContext(ctx).With().Warning("failed to get layer hash", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.AggregatedHash, err = layers.GetAggregatedHash(h.db, lyrID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		h.logger.WithContext(ctx).With().Warning("failed to get aggregated layer hash", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.Ballots, err = ballots.IDsInLayer(h.db, lyrID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		h.logger.WithContext(ctx).With().Warning("failed to get layer ballots", lyrID, log.Err(err))
		return nil, errInternal
	}
	ld.Blocks, err = blocks.IDsInLayer(h.db, lyrID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		h.logger.WithContext(ctx).With().Warning("failed to get layer blocks", lyrID, log.Err(err))
		return nil, errInternal
	}

	out, err := codec.Encode(&ld)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize layer data response", log.Err(err))
	}

	return out, nil
}

// handleLayerOpinionsReq returns the opinions on data in the specified layer, described in LayerOpinions.
func (h *handler) handleLayerOpinionsReq(ctx context.Context, req []byte) ([]byte, error) {
	var (
		lid = types.BytesToLayerID(req)
		ld  LayerOpinions
		out []byte
		err error
	)
	ld.Cert, err = layers.GetCert(h.db, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		h.logger.WithContext(ctx).With().Warning("failed to get certificate for layer", lid, log.Err(err))
		return nil, errInternal
	}
	out, err = codec.Encode(&ld)
	if err != nil {
		h.logger.WithContext(ctx).With().Panic("failed to serialize layer opinions response", log.Err(err))
	}
	return out, nil
}

func (h *handler) handleHashReq(ctx context.Context, data []byte) ([]byte, error) {
	var requestBatch RequestBatch
	err := codec.Decode(data, &requestBatch)
	if err != nil {
		h.logger.WithContext(ctx).With().Error("failed to parse request", log.Err(err))
		return nil, errors.New("bad request")
	}
	resBatch := ResponseBatch{
		ID:        requestBatch.ID,
		Responses: make([]ResponseMessage, 0, len(requestBatch.Requests)),
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
		m := ResponseMessage{
			Hash: r.Hash,
			Data: res,
		}
		resBatch.Responses = append(resBatch.Responses, m)
	}

	bts, err := codec.Encode(&resBatch)
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
