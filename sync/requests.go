package sync

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
)

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 {
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
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 {
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

func blockSliceToChan(blockIds []types.BlockID) chan types.BlockID {
	blockIdsCh := make(chan types.BlockID, len(blockIds))
	for _, id := range blockIds {
		blockIdsCh <- id
	}
	close(blockIdsCh)
	return blockIdsCh
}

func BlockReqFactory() BlockRequestFactory {
	//convert to chan
	return func(s *server.MessageServer, peer p2p.Peer, id types.BlockID) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 {
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
			if len(msg) == 0 {
				s.Warning("peer responded with nil to txs request ", peer)
				return
			}
			var tx []types.SerializableSignedTransaction
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
			if len(msg) == 0 {
				s.Warning("peer responded with nil to atxs request ", peer)
				return
			}
			var tx []types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}

			ch <- calcAndSetIds(tx)
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

func calcAndSetIds(atxs []types.ActivationTx) []types.ActivationTx {
	atxsWithIds := make([]types.ActivationTx, 0, len(atxs))
	for _, atx := range atxs {
		atx.CalcAndSetId()
		atxsWithIds = append(atxsWithIds, atx)
	}
	return atxsWithIds
}

func PoetReqFactory(poetProofRef []byte) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			s.Info("handle PoET proof response")
			defer close(ch)
			if len(msg) == 0 {
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
