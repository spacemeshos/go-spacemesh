package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		lyrid := types.LayerID(util.BytesToUint64(msg))
		logger.With().Info("handle layer hash request", lyrid)
		layer, err := layers.GetLayer(lyrid)
		if err != nil {
			if err == database.ErrNotFound {
				logger.With().Warning("hashes requested for unfamiliar layer ", lyrid)
				return nil
			}
			logger.With().Error("Error handling layer request message", lyrid, log.Err(err))
			return nil
		}

		return layer.Hash().Bytes()
	}
}

func newLayerBlockIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle blockIds request")
		lyrid := types.LayerID(util.BytesToUint64(msg))
		layer, err := layers.GetLayer(lyrid)
		if err != nil {
			if err == database.ErrNotFound {
				logger.With().Warning("block ids requested for unfamiliar layer (id: %s)", lyrid)
				return nil
			}
			logger.With().Error("Error handling ids request message", lyrid, log.Err(err))
			return nil
		}

		blocks := layer.Blocks()

		ids := make([]types.BlockID, 0, len(blocks))
		for _, b := range blocks {
			ids = append(ids, b.ID())
		}

		idbytes, err := types.BlockIdsToBytes(ids)
		if err != nil {
			logger.Error("Error marshaling response message, with blocks IDs: %v and error:", ids, err)
			return nil
		}

		logger.Debug("send ids response layer %v", lyrid)
		return idbytes
	}
}

//todo better logs
func newBlockRequestHandler(msh *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var blockids []types.Hash32
		if err := types.BytesToInterface(msg, &blockids); err != nil {
			logger.Error("Error unmarshalling block request message", err)
			return nil
		}

		var blocks []types.Block
		logger.Info("handle block request ids: %s", concatShortIds(blockids))
		for _, bid := range blockids {
			blockID := types.BlockID(bid.ToHash20())
			logger.With().Info("handle block request", blockID)
			blk, err := msh.GetBlock(blockID)
			if err != nil {
				if err == database.ErrNotFound {
					logger.With().Warning("unfamiliar block was requested (id: %s)", blockID, log.Err(err))
					continue
				}
				logger.With().Error("Error handling block request message", blockID, log.Err(err))
				continue
			}

			blocks = append(blocks, *blk)
		}
		bbytes, err := types.InterfaceToBytes(blocks)
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp) ", err)
			return nil
		}

		logger.Info("send block response")
		return bbytes
	}
}

func newTxsRequestHandler(s *Syncer, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var txids []types.TransactionID
		err := types.BytesToInterface(msg, &txids)
		if err != nil {
			logger.Error("Error marshalling request", err)
			return nil
		}
		logger.With().Info("handle tx request", types.TxIdsField(txids))
		txs, missingDB := s.GetTransactions(txids)

		for t := range missingDB {
			if tx, err := s.txpool.Get(t); err == nil {
				txs = append(txs, tx)
			} else {
				logger.With().Warning("unfamiliar tx was requested", t)
			}
		}

		bbytes, err := types.InterfaceToBytes(txs)
		if err != nil {
			logger.Error("Error marshaling transactions response message, with ids %v and err:", txs, err)
			return nil
		}

		logger.Info("send tx response")
		return bbytes
	}
}

func newAtxsRequestHandler(s *Syncer, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var atxids []types.ATXID
		err := types.BytesToInterface(msg, &atxids)
		if err != nil {
			logger.Error("Unable to marshal request", err)
			return nil
		}
		logger.With().Info("handle atx request", types.AtxIdsField(atxids))
		atxs, unknownAtx := s.GetATXs(atxids)
		for _, t := range unknownAtx {
			if tx, err := s.atxpool.Get(t); err == nil {
				atxs[t] = tx
			} else {
				logger.With().Warning("unfamiliar atx requested", t)
			}
		}

		var transactions []types.ActivationTx
		for _, value := range atxs {
			tx := *value
			transactions = append(transactions, tx)
		}

		bbytes, err := types.InterfaceToBytes(transactions)
		if err != nil {
			logger.Error("Unable to marshal atx response message", err)
			return nil
		}

		logger.Info("send atx response ")
		return bbytes
	}
}

func newPoetRequestHandler(s *Syncer, logger log.Log) func(msg []byte) []byte {
	return func(proofRef []byte) []byte {
		proofMessage, err := s.poetDb.GetProofMessage(proofRef)
		shortPoetRef := proofRef[:util.Min(5, len(proofRef))]
		if err != nil {
			logger.Warning("unfamiliar PoET proof was requested (id: %x): %v", shortPoetRef, err)
			return nil
		}
		logger.Info("returning PoET proof (id: %x) to neighbor", shortPoetRef)
		return proofMessage
	}
}
