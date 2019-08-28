package sync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s WorkerInfra, peer p2p.Peer) (chan interface{}, error) {
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
	return func(s WorkerInfra, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to hash request layer %v", peer, lyr)
				return
			}
			if len(msg) != types.Hash32Length {
				s.Error("received layer hash in wrong length, len %v", len(msg))
				return
			}
			var h types.Hash32
			h.SetBytes(msg)
			ch <- &peerHashPair{peer: peer, hash: h}
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
	return func(s WorkerInfra, peer p2p.Peer, id interface{}) (chan interface{}, error) {
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

			if block.ID() != id {
				s.Info("received block with different id than requested")
				return
			}

			ch <- &block
		}

		bts, err := types.InterfaceToBytes(id) //todo send multiple ids
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(BLOCK, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func TxReqFactory() FetchRequestFactory {
	return func(s WorkerInfra, peer p2p.Peer, ids interface{}) (chan []Item, error) {
		ch := make(chan []Item, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer responded with nil to txs request ", peer)
				return
			}
			var txs []types.SerializableSignedTransaction
			err := types.BytesToInterface(msg, &txs)
			if err != nil || txs == nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}

			if valid, err := validateTxIds(ids.([]types.Hash32), txs); !valid {
				s.Error("fetch failed bad response : %v", err)
				return
			}

			items := make([]Item, len(txs))
			for i, arg := range txs {
				items[i] = &arg
			}
			ch <- items
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

func validateTxIds(ids []types.Hash32, txs []types.SerializableSignedTransaction) (bool, error) {
	mp := make(map[types.Hash32]struct{})
	for _, id := range ids {
		mp[id] = struct{}{}
	}
	for _, tx := range txs {
		txid := types.GetTransactionId(&tx)
		if _, ok := mp[txid.ItemId()]; !ok {
			return false, errors.New(fmt.Sprintf("received a tx that was not requested  %v", hex.EncodeToString(txid[:])))
		}
	}
	return true, nil
}

func ATxReqFactory() FetchRequestFactory {
	return func(s WorkerInfra, peer p2p.Peer, ids interface{}) (chan []Item, error) {
		ch := make(chan []Item, 1)
		foo := func(msg []byte) {
			s.Info("Handle atx response ")
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer responded with nil to atxs request ", peer)
				return
			}
			var atxs []types.ActivationTx
			err := types.BytesToInterface(msg, &atxs)
			if err != nil || atxs == nil {
				s.Error("could not unmarshal atx data %v", err)
				return
			}

			atxs = calcAndSetIds(atxs)
			if valid, err := validateAtxIds(ids.([]types.Hash32), atxs); !valid {
				s.Error("fetch failed bad response : %v", err)
				return
			}

			items := make([]Item, len(atxs))
			for i, arg := range atxs {
				items[i] = arg
			}

			ch <- items
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

func validateAtxIds(ids []types.Hash32, atxs []types.ActivationTx) (bool, error) {
	mp := make(map[types.Hash32]struct{})
	for _, id := range ids {
		mp[id] = struct{}{}
	}
	for _, tx := range atxs {
		txid := tx.ItemId()
		if _, ok := mp[txid]; !ok {
			return false, errors.New(fmt.Sprintf("received a tx that was not requested  %v", tx.ShortId()))
		}
	}
	return true, nil
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
	return func(s WorkerInfra, peer p2p.Peer) (chan interface{}, error) {
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

			if valid, err := validatePoetRef(proofMessage, poetProofRef); !valid {
				s.Error("failed validating poet response", err)
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
		return false, errors.New(fmt.Sprintf("could marshal PoET response for validation %v", err))
	}
	b := sha256.Sum256(poetProofBytes)
	if bytes.Compare(b[:], poetProofRef) != 0 {
		return false, errors.New(fmt.Sprintf("poet recived was diffrent then requested"))
	}

	return true, nil
}
