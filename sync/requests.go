package sync

import (
	"errors"
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
			ch <- ids
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

func BlockReqFactory(blockIds []types.BlockID) RequestFactory {
	//convert to chan
	blockIdsCh := make(chan types.BlockID, len(blockIds))
	for _, id := range blockIds {
		blockIdsCh <- id
	}

	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			block := &types.MiniBlock{}
			err := types.BytesToInterface(msg, block)
			if err != nil {
				s.Error("could not unmarshal Miniblock data")
				return
			}
			ch <- block
		}
		id, ok := <-blockIdsCh
		if !ok {
			return nil, errors.New("chan was closed, job done")
		}

		if err := s.SendRequest(MiniBLOCK, id.ToBytes(), peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

//todo batch requests
func TxReqFactory(ids []types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			var tx []types.SerializableTransaction
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			ch <- tx
		}

		bts, _ := types.InterfaceToBytes(ids) //handle error

		if err := s.SendRequest(TX, bts, peer, foo); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func ATxReqFactory(ids []types.AtxId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			var tx []types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			ch <- tx
		}

		bts, _ := types.InterfaceToBytes(ids) //handle error
		if err := s.SendRequest(ATX, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}
