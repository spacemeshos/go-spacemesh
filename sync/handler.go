package sync

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
)

func newLayerHashRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle layer hash request")
		lyrid := common.BytesToUint64(msg)
		layer, err := layers.GetLayer(types.LayerID(lyrid))
		if err != nil {
			logger.Error("Error handling layer %d request message with error: %v", lyrid, err)
			return nil
		}
		return layer.Hash()
	}
}

func newLayerBlockIdsRequestHandler(layers *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle blockIds request")
		lyrid := common.BytesToUint64(msg)
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

		return idbytes
	}
}

func newMiniBlockRequestHandler(msh *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		logger.Debug("handle block request")
		blockid := types.BlockID(common.BytesToUint64(msg))
		blk, err := msh.GetMiniBlock(blockid)
		if err != nil {
			logger.Error("Error handling Block request message, with BlockID: %d and err: %v", blockid, err)
			return nil
		}

		bbytes, err := types.InterfaceToBytes(*blk)
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp), with BlockID: %d, LayerID: %d and err:", blk.ID(), blk.Layer(), err)
			return nil
		}

		logger.Debug("return block %v", blk)

		return bbytes
	}
}

func newTxsRequestHandler(msh *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var txid types.TransactionId
		copy(txid[:], msg[:32])
		logger.Info("handle tx request %v", hex.EncodeToString(txid[:]))
		txs, _ := msh.GetTransactions([]types.TransactionId{txid})
		if txs == nil {
			logger.Error("Error handling transactions request message, with ids: %d", msg)
			return nil
		}

		//var transactions []*types.SerializableTransaction
		//for _, value := range txs {
		//	transactions = append(transactions, value)
		//}

		bbytes, err := types.InterfaceToBytes(txs[txid])
		if err != nil {
			logger.Error("Error marshaling transactions response message , with ids %v and err:", txs, err)
			return nil
		}

		logger.Debug("tx %v", hex.EncodeToString(bbytes))

		return bbytes
	}
}

func newLayerBlockIdsResponseHandler(ch chan []types.BlockID, logger log.Log) func(msg []byte) {
	return func(msg []byte) {
		defer close(ch)
		ids, err := types.BytesToBlockIds(msg)
		if err != nil {
			logger.Error("could not unmarshal mesh.LayerIDs response")
			return
		}
		lyrIds := make([]types.BlockID, 0, len(ids))
		for _, id := range ids {
			lyrIds = append(lyrIds, id)
		}

		ch <- lyrIds
	}
}

func newMiniBlockResponseHandler(ch chan *types.MiniBlock, logger log.Log) func(msg []byte) {
	return func(msg []byte) {
		defer close(ch)
		logger.Info("handle block response")

		block := &types.MiniBlock{}
		err := types.BytesToInterface(msg, block)
		if err != nil {
			logger.Error("could not unmarshal block data")
			return
		}
		ch <- block
	}
}

func newTxsResponseHandler(ch chan *types.SerializableTransaction, logger log.Log) func(msg []byte) {
	return func(msg []byte) {
		defer close(ch)
		logger.Debug("handle tx response %v", hex.EncodeToString(msg))
		tx := &types.SerializableTransaction{}
		err := types.BytesToInterface(msg, tx)
		if err != nil {
			logger.Error("could not unmarshal tx data %v", err)
			return
		}
		ch <- tx
	}

}
