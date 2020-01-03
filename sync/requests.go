package sync

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"reflect"
)

var (
	BlockFetchReqFactory = newFetchReqFactory(BLOCK, blocksAsItems)
	TxFetchReqFactory    = newFetchReqFactory(TX, txsAsItems)
	AtxFetchReqFactory   = newFetchReqFactory(ATX, atxsAsItems)
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
				s.Error("could not unmarshal mesh.LayerIDs response ", err)
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

func newFetchReqFactory(msgtype server.MessageType, asItems func(msg []byte) ([]Item, error)) BatchRequestFactory {
	//convert to chan
	return func(infra WorkerInfra, peer p2p.Peer, ids []types.Hash32) (chan []Item, error) {
		ch := make(chan []Item, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				infra.Warning("peer %v responded with nil to block request", peer)
				return
			}

			items, err := asItems(msg)
			if err != nil {
				infra.Error("fetch failed bad response : %v", err)
				return
			}

			if valid, err := validateItemIds(ids, items); !valid {
				infra.Error("fetch failed bad response : %v", err)
				return
			}

			ch <- items
		}

		tmr := newFetchRequestTimer(msgtype)
		if err := encodeAndSendRequest(msgtype, ids, infra, peer, foo); err != nil {
			return nil, err
		}
		tmr.ObserveDuration()
		return ch, nil
	}
}

func atxsAsItems(msg []byte) ([]Item, error) {
	var atxs []types.ActivationTx
	err := types.BytesToInterface(msg, &atxs)
	if err != nil || atxs == nil {
		return nil, err
	}
	atxs = calcAndSetIds(atxs)
	items := make([]Item, len(atxs))
	for i := range atxs {
		items[i] = &atxs[i]
	}
	return items, nil
}

func txsAsItems(msg []byte) ([]Item, error) {
	var txs []*types.Transaction
	err := types.BytesToInterface(msg, &txs)
	if err != nil || txs == nil {
		return nil, err
	}
	items := make([]Item, len(txs))
	for i := range txs {
		err := txs[i].CalcAndSetOrigin()
		if err != nil {
			return nil, fmt.Errorf("failed to calc transaction origin (id: %s): %v",
				txs[i].Id().ShortString(), err)
		}
		items[i] = txs[i]
	}
	return items, nil
}

func blocksAsItems(msg []byte) ([]Item, error) {
	var blocks []types.Block
	err := types.BytesToInterface(msg, &blocks)
	if err != nil {
		return nil, err
	}
	items := make([]Item, len(blocks))
	for i := range blocks {
		blocks[i].CalcAndSetId()
		items[i] = &blocks[i]
	}
	return items, nil
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

func validateItemIds(ids []types.Hash32, items []Item) (bool, error) {
	mp := make(map[types.Hash32]struct{})
	for _, id := range ids {
		mp[id] = struct{}{}
	}
	for _, tx := range items {
		txid := tx.Hash32()
		if _, ok := mp[txid]; !ok {
			return false, errors.New(fmt.Sprintf("received item that was not requested %v type %v", tx.ShortString(), reflect.TypeOf(tx)))
		}
		delete(mp, txid)
	}

	if len(mp) > 0 {
		for id, _ := range mp {
			log.Warning("item %s was not in response ", id.ShortString())
		}
	}

	return true, nil
}

func encodeAndSendRequest(req server.MessageType, ids []types.Hash32, s WorkerInfra, peer p2p.Peer, foo func(msg []byte)) error {
	bts, err := types.InterfaceToBytes(ids)
	if err != nil {
		return err
	}
	if err := s.SendRequest(req, bts, peer, foo); err != nil {
		return err
	}
	return nil
}
