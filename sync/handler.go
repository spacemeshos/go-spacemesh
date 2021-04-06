package sync

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lyrid := types.LayerID(util.BytesToUint64(msg))
		lgr := logger.WithContext(ctx).WithFields(lyrid)
		lgr.Info("handle layer hash request")
		layer, err := layers.GetLayer(lyrid)
		if err != nil {
			if err == database.ErrNotFound {
				// we expect this error in the case of a genesis layer
				if types.EpochID(lyrid).IsGenesis() {
					lgr.Info("hashes requested for genesis layer which is not present in database (expected)")
				} else {
					lgr.Warning("hashes requested for unfamiliar layer")
				}
				return nil
			}
			lgr.With().Error("error handling layer request message", log.Err(err))
			return nil
		}

		return layer.Hash().Bytes()
	}
}

func newAtxHashRequestHandler(s *Syncer, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		ep := types.EpochID(util.BytesToUint64(msg))
		logger.WithContext(ctx).With().Info("handle atx hash request", ep)
		atxs := s.atxDb.GetEpochAtxs(ep)
		return types.CalcATXIdsHash32(atxs, nil).Bytes()
	}
}

func newLayerBlockIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lgr := logger.WithContext(ctx)
		lgr.Debug("handle blockIds request")
		lyrid := types.LayerID(util.BytesToUint64(msg))
		layer, err := layers.GetLayer(lyrid)
		if err != nil {
			if err == database.ErrNotFound {
				lgr.With().Warning("block ids requested for unfamiliar layer (id: %s)", lyrid)
				return nil
			}
			lgr.With().Error("error handling ids request message", lyrid, log.Err(err))
			return nil
		}

		blocks := layer.Blocks()

		ids := make([]types.BlockID, 0, len(blocks))
		for _, b := range blocks {
			ids = append(ids, b.ID())
		}

		idbytes, err := types.BlockIdsToBytes(ids)
		if err != nil {
			lgr.Error("error marshaling response message, with blocks IDs: %v and error:", ids, err)
			return nil
		}

		lgr.Debug("send ids response layer %v", lyrid)
		return idbytes
	}
}

func newEpochAtxsRequestHandler(s *Syncer, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lgr := logger.WithContext(ctx)
		lgr.Debug("handle atxid request")
		ep := types.EpochID(util.BytesToUint64(msg))
		atxs := s.atxDb.GetEpochAtxs(ep)

		idbytes, err := types.ATXIdsToBytes(atxs)
		if err != nil {
			lgr.With().Error("error marshaling response message with atx IDs", types.AtxIdsField(atxs), log.Err(err))
			return nil
		}

		lgr.Debug("send atx ids response epoch %v", ep)
		return idbytes
	}
}

//todo better logs
func newBlockRequestHandler(msh *mesh.Mesh, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lgr := logger.WithContext(ctx)
		var blockids []types.Hash32
		if err := types.BytesToInterface(msg, &blockids); err != nil {
			lgr.With().Error("error unmarshalling block request message", log.Err(err))
			return nil
		}

		var blocks []types.Block
		lgr.Info("handle block request ids: %s", concatShortIds(blockids))
		for _, bid := range blockids {
			blockID := types.BlockID(bid.ToHash20())
			lgr.With().Info("handle block request", blockID)
			blk, err := msh.GetBlock(blockID)
			if err != nil {
				if err == database.ErrNotFound {
					lgr.With().Warning("unfamiliar block was requested", blockID, log.Err(err))
					continue
				}
				lgr.With().Error("error handling block request message", blockID, log.Err(err))
				continue
			}

			blocks = append(blocks, *blk)
		}
		bbytes, err := types.InterfaceToBytes(blocks)
		if err != nil {
			lgr.With().Error("error marshaling response message (FetchBlockResp)", log.Err(err))
			return nil
		}

		lgr.Info("send block response")
		return bbytes
	}
}

func newTxsRequestHandler(s *Syncer, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lgr := logger.WithContext(ctx)
		var txids []types.TransactionID
		if err := types.BytesToInterface(msg, &txids); err != nil {
			lgr.With().Error("error marshalling request", log.Err(err))
			return nil
		}
		lgr.With().Info("handle tx request", types.TxIdsField(txids))
		txs, missingDB := s.GetTransactions(txids)

		for t := range missingDB {
			if tx, err := s.txpool.Get(t); err == nil {
				txs = append(txs, tx)
			} else {
				lgr.With().Warning("unfamiliar tx was requested", t)
			}
		}

		bbytes, err := types.InterfaceToBytes(txs)
		if err != nil {
			lgr.Error("Error marshaling transactions response message, with ids %v and err:", txs, err)
			return nil
		}

		lgr.Info("send tx response")
		return bbytes
	}
}

func newAtxsRequestHandler(s *Syncer, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, msg []byte) []byte {
		lgr := logger.WithContext(ctx)
		var atxids []types.ATXID
		if err := types.BytesToInterface(msg, &atxids); err != nil {
			lgr.With().Error("unable to marshal request", log.Err(err))
			return nil
		}
		lgr.With().Info("handle atx request", types.AtxIdsField(atxids))
		atxs, unknownAtx := s.GetATXs(ctx, atxids)
		for _, t := range unknownAtx {
			if tx, err := s.atxDb.GetFullAtx(t); err == nil {
				atxs[t] = tx
			} else {
				lgr.With().Warning("unfamiliar atx requested", t)
			}
		}

		var transactions []types.ActivationTx
		for _, value := range atxs {
			tx := *value
			transactions = append(transactions, tx)
		}

		bbytes, err := types.InterfaceToBytes(transactions)
		if err != nil {
			lgr.Error("unable to marshal atx response message", err)
			return nil
		}

		lgr.Info("send atx response")
		return bbytes
	}
}

func newPoetRequestHandler(s *Syncer, logger log.Log) func(context.Context, []byte) []byte {
	return func(ctx context.Context, proofRef []byte) []byte {
		lgr := logger.WithContext(ctx)
		proofMessage, err := s.poetDb.GetProofMessage(proofRef)
		shortPoetRef := proofRef[:util.Min(5, len(proofRef))]
		if err != nil {
			lgr.Warning("unfamiliar poet proof was requested (id: %x): %v", shortPoetRef, err)
			return nil
		}
		lgr.Info("returning poet proof (id: %x) to neighbor", shortPoetRef)
		return proofMessage
	}
}
