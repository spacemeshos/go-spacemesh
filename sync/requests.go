package sync

import (
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
)

func layerHashRequest(peer p2p.Peer) (chan interface{}, func(msg []byte)) {
	ch := make(chan interface{}, 1)
	foo := func(msg []byte) {
		defer close(ch)
		ch <- &peerHashPair{peer: peer, hash: msg}
	}
	return ch, foo
}

func layerBlockIDsRequest() (chan interface{}, func(msg []byte)) {
	ch := make(chan interface{}, 1)
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

func miniBlockRequest() (chan interface{}, func(msg []byte)) {
	ch := make(chan interface{}, 1)
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

func txRequest() (chan interface{}, func(msg []byte)) {
	ch := make(chan interface{}, 1)
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

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch, foo := layerBlockIDsRequest()
		if err := s.SendRequest(LAYER_IDS, lyr.ToBytes(), peer, foo); err != nil {
			log.Error("could not get layer ", lyr, " hash from peer ", peer)
			return nil, errors.New("error ")
		}
		return ch, nil
	}
}

func HashReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send layer hash request Peer: %v layer: %v", peer, lyr)
		ch, foo := layerHashRequest(peer)
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, foo); err != nil {
			s.Error("could not get layer ", lyr, " hash from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}

}

func BlocReqFactory(id types.BlockID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send block request Peer: %v layer: %v", peer, id)
		ch, foo := miniBlockRequest()
		if err := s.SendRequest(BLOCK, id.ToBytes(), peer, foo); err != nil {
			s.Error("could not get block ", id, " hash from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}
}

//todo batch requests
func TxReqFactory(id types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send block request Peer: %v layer: %v", peer, id)
		ch, foo := txRequest()
		if err := s.SendRequest(TX, id[:], peer, foo); err != nil {
			s.Error("could not get transaction ", id, "  from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}
}
