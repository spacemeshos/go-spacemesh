package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		lyrid := util.BytesToUint64(msg)
		logger.With().Info("handle layer hash request", log.LayerId(lyrid))
		layer, err := layers.GetLayer(types.LayerID(lyrid))
		if err != nil {
			logger.With().Error("Error handling layer request message", log.LayerId(lyrid), log.Err(err))
			return nil
		}
		hash := layer.Hash()
		return hash[:]
	}
}

func newLayerBlockIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle blockIds request")
		lyrid := util.BytesToUint64(msg)
		layer, err := layers.GetLayer(types.LayerID(lyrid))
		if err != nil {
			logger.Error("Error handling ids request message with LayerID: %d and error: %s", lyrid, err.Error())
			return nil
		}

		blocks := layer.Blocks()

		ids := make([]types.BlockID, 0, len(blocks))
		for _, b := range blocks {
			ids = append(ids, b.ID())
		}

		idbytes, err := types.BlockIdsAsBytes(ids)
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
			logger.Error("Error handling block request message", err)
			return nil
		}

		var blocks []types.Block
		for _, bid := range blockids {
			var id = util.BytesToUint64(bid.Bytes())
			logger.Debug("handle block %v request", id)
			blk, err := msh.GetBlock(types.BlockID(id))
			if err != nil {
				logger.Error("Error handling block request message, with BlockID: %d and err: %v", id, err)
				continue
			}
			blocks = append(blocks, *blk)
		}
		bbytes, err := types.InterfaceToBytes(blocks)
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp) ", err)
			return nil
		}

		logger.Debug("send block response")
		return bbytes
	}
}

func newTxsRequestHandler(s *Syncer, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var txids []types.TransactionId
		err := types.BytesToInterface(msg, &txids)
		if err != nil {
			logger.Error("Error marshalling request", err)
			return nil
		}
		logger.Info("handle tx request ")
		txs, missingDB := s.GetTransactions(txids)

		for t := range missingDB {
			if tx, err := s.txpool.Get(t); err == nil {
				txs = append(txs, &tx)
			} else {
				logger.Error("Error handling tx request message, for id: %v", t.ShortString())
			}
		}

		bbytes, err := types.InterfaceToBytes(txs)
		if err != nil {
			logger.Error("Error marshaling transactions response message , with ids %v and err:", txs, err)
			return nil
		}

		logger.Info("send tx response ")
		return bbytes
	}
}

func newATxsRequestHandler(s *Syncer, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var atxids []types.AtxId
		err := types.BytesToInterface(msg, &atxids)
		if err != nil {
			logger.Error("Error marshalling request", err)
			return nil
		}
		logger.Info("handle atx request ")
		atxs, missinDB := s.GetATXs(atxids)
		for _, t := range missinDB {
			if tx, err := s.atxpool.Get(t); err == nil {
				atxs[t] = tx
			} else {
				logger.With().Error("error handling atx request message", log.AtxId(t.ShortString()), log.String("atx_ids", fmt.Sprintf("%x", atxids)))
			}
		}

		var transactions []types.ActivationTx
		for _, value := range atxs {
			tx := *value
			transactions = append(transactions, tx)
		}

		bbytes, err := types.InterfaceToBytes(transactions)
		if err != nil {
			logger.Error("Error marshaling atx response message , with ids %v and err:", atxs, err)
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
