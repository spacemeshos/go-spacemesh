package sync

import (
	"bytes"
	"crypto/sha256"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
)

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			defer close(ch)
			if msg == nil {
				s.Info("received empty layer ids response")
				return
			}

			ids, err := types.BytesToBlockIds(msg)
			if err != nil {
				s.Error("could not unmarshal layer ids response: %v", err)
				return
			}

			ch <- ids
		}
		if err := s.SendRequest(LAYER_IDS, lyr.ToBytes(), peer, resHandler); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func HashReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			defer close(ch)
			if msg == nil {
				s.Info("received empty layer hash response")
				return
			}

			ch <- &peerHashPair{peer: peer, hash: msg}
		}
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, resHandler); err != nil {
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
		resHandler := func(msg []byte) {
			defer close(ch)
			if msg == nil {
				s.Info("received empty block data response")
				return
			}

			var block types.Block
			err := types.BytesToInterface(msg, &block)
			if err != nil {
				s.Error("could not unmarshal block data response: %v", err)
				return
			}

			if block.ID() != id {
				s.Info("received block with different id than requested")
				return
			}

			ch <- &block
		}

		if err := s.SendRequest(BLOCK, id.ToBytes(), peer, resHandler); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

//todo batch requests
func TxReqFactory(ids []types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			defer close(ch)
			if msg == nil {
				s.Info("received empty tx data response")
				return
			}

			var txs []types.SerializableSignedTransaction
			err := types.BytesToInterface(msg, &txs)
			if err != nil {
				s.Error("could not unmarshal tx data response: %v", err)
				return
			}

			mp := make(map[types.TransactionId]struct{})
			for _, id := range ids {
				mp[id] = struct{}{}
			}

			for _, tx := range txs {
				txid := types.GetTransactionId(&tx)
				if _, ok := mp[txid]; !ok {
					s.Info("received a tx that was not requested  %v", txid)
					return
				}
			}

			ch <- txs
		}

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(TX, bts, peer, resHandler); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func ATxReqFactory(ids []types.AtxId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			defer close(ch)
			if msg == nil {
				s.Info("received empty atx data response")
				return
			}

			var atxs []types.ActivationTx
			err := types.BytesToInterface(msg, &atxs)
			if err != nil {
				s.Error("could not unmarshal atx data response: %v", err)
				return
			}

			mp := make(map[types.AtxId]struct{})
			for _, id := range ids {
				mp[id] = struct{}{}
			}

			atxs = calcAndSetIds(atxs)

			for _, tx := range atxs {
				txid := tx.Id()
				if _, ok := mp[txid]; !ok {
					s.Info("received an atx that was not requested  %v", txid)
					return
				}
			}

			ch <- atxs
		}

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}
		if err := s.SendRequest(ATX, bts, peer, resHandler); err != nil {
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
			defer close(ch)
			if msg == nil {
				s.Info("received empty PoET proof response")
				return
			}

			var proofMessage types.PoetProofMessage
			err := types.BytesToInterface(msg, &proofMessage)
			if err != nil {
				s.Error("could not unmarshal PoET proof response: %v", err)
				return
			}

			valid, err := validatePoetRef(proofMessage, poetProofRef)
			if err != nil {
				s.Error("could not unmarshal PoET proof response: %v", err)
				return
			}

			if valid {
				s.Info("received an atx that was not requested  %v", poetProofRef)
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

func validatePoetRef(proofMessage types.PoetProofMessage, poetProofRef []byte) (bool, error) {
	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		log.Error("could marshal PoET response for validation ", err)
		return false, err
	}
	b := sha256.Sum256(poetProofBytes)
	if bytes.Compare(b[:], poetProofRef) != 0 {
		return false, err
	}

	return true, nil
}
