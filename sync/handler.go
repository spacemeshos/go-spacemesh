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
			logger.Error("Error handling MiniBlock request message, with BlockID: %d and err: %v", blockid, err)
			return nil
		}

		bbytes, err := types.InterfaceToBytes(*blk)
		if err != nil {
			logger.Error("Error marshaling response message (FetchBlockResp), with BlockID: %d, LayerID: %d and err:", blk.ID(), blk.Layer(), err)
			return nil
		}

		logger.Debug("return block %v", blk.ID())

		return bbytes
	}
}

func newTxsRequestHandler(msh *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var txids []types.TransactionId
		err := types.BytesToInterface(msg, &txids)
		if err != nil {
			logger.Error("Error marshalling request", err)
			return nil
		}
		logger.Info("handle tx request ")
		txs, missed := msh.GetTransactions(txids)
		if len(missed)  > 0 {
			logger.Error("Error handling transactions request message, with ids: %d", msg)
			return nil
		}

		var transactions []types.SerializableTransaction
		for _, value := range txs {
			transactions = append(transactions, *value)
		}

		bbytes, err := types.InterfaceToBytes(transactions)
		if err != nil {
			logger.Error("Error marshaling transactions response message , with ids %v and err:", txs, err)
			return nil
		}

		logger.Debug("tx %v", hex.EncodeToString(bbytes))

		return bbytes
	}
}

func newATxsRequestHandler(msh *mesh.Mesh, logger log.Log) func(msg []byte) []byte {
	return func(msg []byte) []byte {
		var txids []types.AtxId
		err := types.BytesToInterface(msg, &txids)
		if err != nil {
			logger.Error("Error marshalling request", err)
			return nil
		}
		logger.Info("handle atx request ")
		txs, missed := msh.GetATXs(txids)
		if len(missed) > 0 {
			logger.Error("Error handling atx request message, with ids: %d", msg)
			return nil
		}

		var transactions []types.ActivationTx
		for _, value := range txs {
			value.Nipst, err = msh.GetNipst(value.Id())
			if err != nil {
				logger.Error("Error handling atx request message, cannot find nipst for atx %v" , value.Id())
				return nil
			}
			transactions = append(transactions, *value)
		}

		bbytes, err := types.InterfaceToBytes(transactions)
		if err != nil {
			logger.Error("Error marshaling atx response message , with ids %v and err:", txs, err)
			return nil
		}

		logger.Debug("tx %v", hex.EncodeToString(bbytes))
		logger.Info("send atx response ")
		return bbytes
	}
}
