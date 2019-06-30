package sync

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
)

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

func blockSliceToChan(blockIds []types.BlockID) chan types.BlockID {
	blockIdsCh := make(chan types.BlockID, len(blockIds))
	for _, id := range blockIds {
		blockIdsCh <- id
	}
	return blockIdsCh
}

func BlockReqFactory(blockIds chan types.BlockID) RequestFactory {

	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			var block types.Block
			err := types.BytesToInterface(msg, &block)
			if err != nil {
				s.Error("could not unmarshal Miniblock data", err)
				return
			}
			ch <- &block
		}
		id, ok := <-blockIds
		if !ok {
			return nil, errors.New("chan was closed, job done")
		}

		if err := s.SendRequest(MINI_BLOCK, id.ToBytes(), peer, foo); err != nil {
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

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}

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
			s.Info("handle atx response ")
			defer close(ch)
			var tx []types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}

			ch <- tx
		}

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}
		if err := s.SendRequest(ATX, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}
