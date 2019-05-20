package sync

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
)

func layerHashRequest(peer p2p.Peer, layer types.LayerID) (chan *peerHashPair, func(msg []byte)) {
	ch := make(chan *peerHashPair, 1)
	foo := func(msg []byte) {
		defer close(ch)
		ch <- &peerHashPair{peer: peer, hash: msg}
	}
	return ch, foo
}

func layerBlockIDsRequest() (chan []types.BlockID, func(msg []byte)) {
	ch := make(chan []types.BlockID, 1)
	foo := func(msg []byte) {
		defer close(ch)
		ids, err := types.BytesToBlockIds(msg)
		if err != nil {
			log.Error("could not unmarshal mesh.LayerIDs response")
			return
		}
		lyrIds := make([]types.BlockID, 0, len(ids))
		for _, id := range ids {
			lyrIds = append(lyrIds, id)
		}

		ch <- lyrIds
	}
	return ch, foo
}

func miniBlockRequest() (chan *types.MiniBlock, func(msg []byte)) {
	ch := make(chan *types.MiniBlock, 1)
	foo := func(msg []byte) {
		defer close(ch)
		log.Info("handle block response")

		block := &types.MiniBlock{}
		err := types.BytesToInterface(msg, block)
		if err != nil {
			log.Error("could not unmarshal block data")
			return
		}
		ch <- block
	}
	return ch, foo
}

func txRequest() (chan *types.SerializableTransaction, func(msg []byte)) {
	ch := make(chan *types.SerializableTransaction, 1)
	foo := func(msg []byte) {
		defer close(ch)
		log.Debug("handle tx response %v", hex.EncodeToString(msg))
		tx := &types.SerializableTransaction{}
		err := types.BytesToInterface(msg, tx)
		if err != nil {
			log.Error("could not unmarshal tx data %v", err)
			return
		}
		ch <- tx
	}
	return ch, foo
}

func blockRequest() (chan *types.Block, func(msg []byte)) {
	ch := make(chan *types.Block, 1)
	foo := func(msg []byte) {
		defer close(ch)
		block := &types.Block{}
		err := types.BytesToInterface(msg, block)
		if err != nil {
			log.Error("could not unmarshal block data")
			return
		}
		ch <- block
	}

	return ch, foo
}
