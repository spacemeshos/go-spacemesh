package sync

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
)

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to layer %v request", peer, lyr)
				return
			}
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
	return func(s *MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to hash request layer %v", peer, lyr)
				return
			}
			ch <- &peerHashPair{peer: peer, hash: msg}
		}
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}

}

func blockSliceToChan(blockIds []types.BlockID) chan interface{} {
	blockIdsCh := make(chan interface{}, len(blockIds))
	for _, id := range blockIds {
		blockIdsCh <- id
	}
	close(blockIdsCh)
	return blockIdsCh
}

func BlockReqFactory() RequestFactoryV2 {
	//convert to chan
	return func(s *MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to block %v request", peer, id)
				return
			}
			var block types.Block
			err := types.BytesToInterface(msg, &block)
			if err != nil {
				s.Error("could not unmarshal block data", err)
				return
			}
			ch <- &block
		}

		bts, err := types.InterfaceToBytes(id) //todo send multiple ids
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(MINI_BLOCK, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func TxReqFactory() RequestFactoryV2 {
	return func(s *MessageServer, peer p2p.Peer, ids interface{}) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer responded with nil to txs request ", peer)
				return
			}
			var tx []*types.SerializableSignedTransaction
			err := types.BytesToInterface(msg, &tx)
			if err != nil || tx == nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			ch <- tx
		}

		bts, err := types.InterfaceToBytes(ids) //todo send multiple ids
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(TX, bts, peer, foo); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func ATxReqFactory() RequestFactoryV2 {
	return func(s *MessageServer, peer p2p.Peer, ids interface{}) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			s.Info("Handle atx response ")
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer responded with nil to atxs request ", peer)
				return
			}
			var tx []*types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil || tx == nil {
				s.Error("could not unmarshal atx data %v", err)
				return
			}

			ch <- tx
		}

		bts, err := types.InterfaceToBytes(ids) //todo send multiple ids
		if err != nil {
			return nil, err
		}
		if err := s.SendRequest(ATX, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func PoetReqFactory(poetProofRef []byte) RequestFactory {
	return func(s *MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			s.Info("handle PoET proof response")
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer responded with nil to poet request %v", peer, poetProofRef)
				return
			}

			var proofMessage types.PoetProofMessage
			err := types.BytesToInterface(msg, &proofMessage)
			if err != nil {
				s.Error("could not unmarshal PoET proof message: %v", err)
				return
			}

			ch <- proofMessage
		}

		if err := s.SendRequest(POET, poetProofRef, peer, resHandler); err != nil {
			return nil, err
		}

		return ch, nil
	}
}
