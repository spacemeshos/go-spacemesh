package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

const (
	fetchSubKey sql.QueryCacheSubKey = "epoch-info-req"
)

type handler struct {
	logger *zap.Logger
	cdb    *datastore.CachedDB
	bs     *datastore.BlobStore
}

func newHandler(
	cdb *datastore.CachedDB,
	bs *datastore.BlobStore,
	lg *zap.Logger,
) *handler {
	return &handler{
		logger: lg,
		cdb:    cdb,
		bs:     bs,
	}
}

// handleMaliciousIDsReq returns the IDs of all known malicious nodes.
func (h *handler) handleMaliciousIDsReq(ctx context.Context, _ []byte) ([]byte, error) {
	nodes, err := identities.All(h.cdb)
	if err != nil {
		return nil, fmt.Errorf("getting malicious IDs: %w", err)
	}
	h.logger.Debug("responded to malicious IDs request", log.ZContext(ctx), zap.Int("num_malicious", len(nodes)))
	malicious := &MaliciousIDs{
		NodeIDs: nodes,
	}
	return codec.MustEncode(malicious), nil
}

func (h *handler) handleMaliciousIDsReqStream(ctx context.Context, msg []byte, s io.ReadWriter) error {
	if err := h.streamIDs(ctx, s, func(cbk retrieveCallback) error {
		nodeIDs, err := identities.All(h.cdb)
		if err != nil {
			return fmt.Errorf("getting malicious IDs: %w", err)
		}
		for _, nodeID := range nodeIDs {
			cbk(len(nodeIDs), nodeID[:])
		}
		return nil
	}); err != nil {
		h.logger.Debug("failed to stream malicious node IDs", log.ZContext(ctx), zap.Error(err))
	}

	return nil
}

// handleEpochInfoReq returns the ATXs published in the specified epoch.
func (h *handler) handleEpochInfoReq(ctx context.Context, msg []byte) ([]byte, error) {
	var epoch types.EpochID
	if err := codec.Decode(msg, &epoch); err != nil {
		return nil, err
	}

	cacheKey := sql.QueryCacheKey(atxs.CacheKindEpochATXs, epoch.String())
	return sql.WithCachedSubKey(ctx, h.cdb, cacheKey, fetchSubKey, func(ctx context.Context) ([]byte, error) {
		atxids, err := atxs.GetIDsByEpoch(ctx, h.cdb, epoch)
		if err != nil {
			return nil, fmt.Errorf("getting ATX IDs: %w", err)
		}
		ed := EpochData{
			AtxIDs: atxids,
		}
		bts, err := codec.Encode(&ed)
		if err != nil {
			h.logger.Fatal("failed to serialize EpochData",
				zap.Uint32("epoch", epoch.Uint32()),
				log.ZContext(ctx),
				zap.Error(err),
			)
		}
		return bts, nil
	})
}

// handleEpochInfoReq streams the ATXs published in the specified epoch.
func (h *handler) handleEpochInfoReqStream(ctx context.Context, msg []byte, s io.ReadWriter) error {
	var epoch types.EpochID
	if err := codec.Decode(msg, &epoch); err != nil {
		return err
	}
	if err := h.streamIDs(ctx, s, func(cbk retrieveCallback) error {
		atxids, err := atxs.GetIDsByEpoch(ctx, h.cdb, epoch)
		if err != nil {
			return fmt.Errorf("getting ATX IDs: %w", err)
		}
		for _, atxID := range atxids {
			cbk(len(atxids), atxID[:])
		}
		return nil
	}); err != nil {
		h.logger.Debug("failed to stream epoch atx IDs",
			log.ZContext(ctx),
			zap.Uint32("epoch", epoch.Uint32()),
			zap.Error(err),
		)
	}

	return nil
}

type (
	retrieveCallback func(total int, id []byte) error
	retrieveFunc     func(retrieveCallback) error
)

func (h *handler) streamIDs(ctx context.Context, s io.ReadWriter, retrieve retrieveFunc) error {
	started := false
	if err := retrieve(func(total int, id []byte) error {
		if !started {
			started = true
			respSize := scale.LenSize(uint32(total)) + uint32(total*len(id))
			if _, err := codec.EncodeLen(s, respSize); err != nil {
				return err
			}
			if _, err := codec.EncodeLen(s, uint32(total)); err != nil {
				return err
			}
		}
		if _, err := s.Write(id[:]); err != nil {
			return err
		}
		return nil
	},
	); err != nil {
		if !started {
			if err := server.WriteErrorResponse(s, err); err != nil {
				h.logger.Debug("failed to write error response", log.ZContext(ctx), zap.Error(err))
			}
		}
		return err
	}

	// If any IDs were sent:
	// Response.Data already sent
	// Response.Error has length 0
	lens := []uint32{0}
	if !started {
		// If no ATX IDs were sent:
		// Response.Data is just a single zero byte (length 0),
		// but the length of Response.Data is 1 so we must send it
		// Response.Error has length 0
		lens = []uint32{1, 0, 0}
	}
	for _, l := range lens {
		if _, err := codec.EncodeLen(s, l); err != nil {
			return err
		}
	}

	return nil
}

// handleLayerDataReq returns all data in a layer, described in LayerData.
func (h *handler) handleLayerDataReq(ctx context.Context, req []byte) ([]byte, error) {
	var (
		lid types.LayerID
		ld  LayerData
		err error
	)
	if err := codec.Decode(req, &lid); err != nil {
		return nil, err
	}
	ld.Ballots, err = ballots.IDsInLayer(h.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("getting ballots for layer %d: %w", lid, err)
	}

	out, err := codec.Encode(&ld)
	if err != nil {
		h.logger.Fatal("failed to serialize layer data response", log.ZContext(ctx), zap.Error(err))
	}
	return out, nil
}

func (h *handler) handleLayerOpinionsReq2(ctx context.Context, data []byte) ([]byte, error) {
	var req OpinionRequest
	if err := codec.Decode(data, &req); err != nil {
		return nil, err
	}
	if req.Block != nil {
		return h.handleCertReq(ctx, req.Layer, *req.Block)
	}

	var (
		lid = req.Layer
		lo  LayerOpinion
		out []byte
		err error
	)

	opnReqV2.Inc()
	lo.PrevAggHash, err = layers.GetAggregatedHash(h.cdb, lid.Sub(1))
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("getting aggregated hash for layer %d: %w", lid.Sub(1), err)
	}
	bid, err := certificates.CertifiedBlock(h.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("getting certified block for layer %d: %w", lid, err)
	}
	if err == nil {
		lo.Certified = &bid
	}
	out, err = codec.Encode(&lo)
	if err != nil {
		h.logger.Fatal("failed to serialize layer opinions response", log.ZContext(ctx), zap.Error(err))
	}
	return out, nil
}

func (h *handler) handleCertReq(ctx context.Context, lid types.LayerID, bid types.BlockID) ([]byte, error) {
	certReq.Inc()
	certs, err := certificates.Get(h.cdb, lid)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, fmt.Errorf("getting certificates for layer %d: %w", lid, err)
	}
	if err == nil {
		for _, cert := range certs {
			if cert.Block == bid {
				out, err := codec.Encode(cert.Cert)
				if err != nil {
					h.logger.Fatal("failed to encode cert", log.ZContext(ctx), zap.Error(err))
				}
				return out, nil
			}
		}
	}
	return nil, err
}

func (h *handler) handleHashReq(ctx context.Context, data []byte) ([]byte, error) {
	return h.doHandleHashReq(ctx, data, datastore.NoHint)
}

func (h *handler) doHandleHashReq(ctx context.Context, data []byte, hint datastore.Hint) ([]byte, error) {
	var requestBatch RequestBatch
	if err := codec.Decode(data, &requestBatch); err != nil {
		return nil, fmt.Errorf("%w: decoding request: %w", errBadRequest, err)
	}

	if hint != datastore.NoHint && len(requestBatch.Requests) > 1 {
		return nil, fmt.Errorf("batch of size 1 expected for %s", hint)
	}

	resBatch := ResponseBatch{
		ID:        requestBatch.ID,
		Responses: make([]ResponseMessage, 0, len(requestBatch.Requests)),
	}
	// this will iterate all requests and populate appropriate Responses, if there are any missing items they will not
	// be included in the response at all
	for _, r := range requestBatch.Requests {
		if hint != datastore.NoHint && r.Hint != hint {
			return nil, fmt.Errorf("bad hint: %q (expected %q)", r.Hint, hint)
		}
		totalHashReqs.WithLabelValues(string(r.Hint)).Add(1)
		var blob sql.Blob
		if err := h.bs.LoadBlob(ctx, r.Hint, r.Hash.Bytes(), &blob); err != nil {
			if !errors.Is(err, datastore.ErrNotFound) {
				return nil, fmt.Errorf("loading blob of %s (hint: %s): %w", r.Hash, r.Hint, err)
			}
			h.logger.Debug("remote peer requested nonexistent hash",
				log.ZContext(ctx),
				zap.Stringer("hash", r.Hash),
				zap.String("hint", string(r.Hint)))
			hashMissing.WithLabelValues(string(r.Hint)).Add(1)
			continue
		} else if len(blob.Bytes) == 0 {
			h.logger.Debug("remote peer requested golden", log.ZContext(ctx), zap.Stringer("hash", r.Hash))
			hashEmptyData.WithLabelValues(string(r.Hint)).Add(1)
			continue
		} else {
			h.logger.Debug("responded to hash request",
				log.ZContext(ctx),
				zap.Stringer("hash", r.Hash),
				zap.Int("dataSize", len(blob.Bytes)))
		}
		// add response to batch
		m := ResponseMessage{
			Hash: r.Hash,
			Data: blob.Bytes,
		}
		resBatch.Responses = append(resBatch.Responses, m)
	}

	bts, err := codec.Encode(&resBatch)
	if err != nil {
		h.logger.Fatal("failed to encode batch id",
			log.ZContext(ctx),
			zap.Error(err),
			zap.Stringer("batch_hash", resBatch.ID))
		return nil, err
	}
	h.logger.Debug("returning response for batch",
		log.ZContext(ctx),
		zap.Stringer("batch_hash", resBatch.ID),
		zap.Int("count_responses", len(resBatch.Responses)),
		zap.Int("data_size", len(bts)))
	return bts, nil
}

func (h *handler) handleHashReqStream(ctx context.Context, msg []byte, s io.ReadWriter) error {
	return h.doHandleHashReqStream(ctx, msg, s, datastore.NoHint)
}

func (h *handler) doHandleHashReqStream(
	ctx context.Context,
	msg []byte,
	s io.ReadWriter,
	hint datastore.Hint,
) error {
	var requestBatch RequestBatch
	if err := codec.Decode(msg, &requestBatch); err != nil {
		return fmt.Errorf("%w: decooding request: %w", errBadRequest, err)
	}

	if hint != datastore.NoHint && len(requestBatch.Requests) > 1 {
		return fmt.Errorf("batch of size 1 expected for %s", hint)
	}

	idsByHint := make(map[datastore.Hint][][]byte)
	for _, r := range requestBatch.Requests {
		if hint != datastore.NoHint && r.Hint != hint {
			return fmt.Errorf("bad hint: %q (expected %q)", r.Hint, hint)
		}
		idsByHint[r.Hint] = append(idsByHint[r.Hint], r.Hash.Bytes())
	}

	totalSize := uint32(types.Hash32Length)
	var count uint32
	for hint, ids := range idsByHint {
		goodIDs := make([][]byte, 0, len(ids))
		sizes, err := h.bs.GetBlobSizes(hint, ids)
		if err != nil {
			// At this point, nothing has been written yet, so we can report
			// and error
			if err := server.WriteErrorResponse(s, err); err != nil {
				return err
			}
			return nil
		}
		for n, size := range sizes {
			if size > 0 {
				goodIDs = append(goodIDs, ids[n])
				count++
				totalSize += types.Hash32Length + scale.LenSize(uint32(size)) + uint32(size)
			}
		}
		idsByHint[hint] = goodIDs
	}

	totalSize += scale.LenSize(count)

	if _, err := codec.EncodeLen(s, totalSize); err != nil {
		return err
	}

	if _, err := codec.EncodeTo(s, &requestBatch.ID); err != nil {
		return err
	}

	if _, err := codec.EncodeLen(s, count); err != nil {
		return err
	}

	var blob sql.Blob
	for hint, ids := range idsByHint {
		for _, id := range ids {
			if err := h.bs.LoadBlob(ctx, hint, id, &blob); err != nil {
				return err
			}
			if _, err := s.Write(id); err != nil {
				return err
			}
			if _, err := codec.EncodeLen(s, uint32(len(blob.Bytes))); err != nil {
				return err
			}
			if _, err := s.Write(blob.Bytes); err != nil {
				return err
			}
		}
	}

	if _, err := codec.EncodeLen(s, 0); err != nil {
		return err
	}

	return nil
}

func (h *handler) handleMeshHashReq(ctx context.Context, reqData []byte) ([]byte, error) {
	var (
		req    MeshHashRequest
		hashes []types.Hash32
		data   []byte
		err    error
	)
	if err = codec.Decode(reqData, &req); err != nil {
		return nil, fmt.Errorf("%w: decoding request: %w", errBadRequest, err)
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validating request: %w", err)
	}
	hashes, err = layers.GetAggHashes(h.cdb, req.From, req.To, req.Step)
	if err != nil {
		return nil, err
	}
	data, err = codec.EncodeSlice(hashes)
	if err != nil {
		h.logger.Fatal("failed to encode hashes", log.ZContext(ctx), zap.Error(err))
	}
	h.logger.Debug("returning response for mesh hashes",
		log.ZContext(ctx),
		zap.Stringer("layer_from", req.From),
		zap.Stringer("layer_to", req.To),
		zap.Uint32("by", req.Step),
		zap.Int("count_hashes", len(hashes)),
	)
	return data, nil
}

func (h *handler) handleMeshHashReqStream(ctx context.Context, reqData []byte, s io.ReadWriter) error {
	var req MeshHashRequest
	if err := codec.Decode(reqData, &req); err != nil {
		return fmt.Errorf("%w: decoding request: %w", errBadRequest, err)
	}

	if err := h.streamIDs(ctx, s, func(cbk retrieveCallback) error {
		if err := req.Validate(); err != nil {
			return fmt.Errorf("validating request: %w", err)
		}

		hashes, err := layers.GetAggHashes(h.cdb, req.From, req.To, req.Step)
		if err != nil {
			return err
		}
		for _, id := range hashes {
			cbk(len(hashes), id[:])
		}
		return nil
	}); err != nil {
		h.logger.Debug("failed to stream mesh hashes", log.ZContext(ctx), zap.Error(err))
	}

	return nil
}
