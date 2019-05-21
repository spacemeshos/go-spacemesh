package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
)

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
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			ids, err := types.BytesToBlockIds(msg)
			if err != nil {
				s.Error("could not unmarshal mesh.LayerIDs response")
				return
			}
			lyrIds := make([]types.BlockID, 0, len(ids))
			for _, id := range ids {
				lyrIds = append(lyrIds, id)
			}

			ch <- lyrIds
		}
		if err := s.SendRequest(LAYER_IDS, lyr.ToBytes(), peer, foo); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func HashReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			ch <- &peerHashPair{peer: peer, hash: msg}
		}
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}

}

func BlocReqFactory(blockIds chan types.BlockID) RequestFactory {

	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			block := &types.MiniBlock{}
			err := types.BytesToInterface(msg, block)
			if err != nil {
				s.Error("could not unmarshal block data")
				return
			}
			ch <- block
		}
		id := <-blockIds
		if err := s.SendRequest(BLOCK, id.ToBytes(), peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

//todo batch requests
func TxReqFactory(id types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			tx := &types.SerializableTransaction{}
			err := types.BytesToInterface(msg, tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			ch <- tx
		}
		if err := s.SendRequest(TX, id[:], peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}
