package sync

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

var (
	blockFetchReqFactory = newFetchReqFactory(blockMsg, blocksAsItems)
	txFetchReqFactory    = newFetchReqFactory(txMsg, txsAsItems)
	atxFetchReqFactory   = newFetchReqFactory(atxMsg, atxsAsItems)
)

func layerIdsReqFactory(lyr types.LayerID) requestFactory {
	return func(s networker, peer p2ppeers.Peer) (chan interface{}, error) {
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
		if err := s.SendRequest(layerIdsMsg, lyr.Bytes(), peer, foo, func(err error) {}); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func getEpochAtxIds(epoch types.EpochID, s networker, peer p2ppeers.Peer) (chan interface{}, error) {
	ch := make(chan interface{}, 1)
	foo := func(msg []byte) {
		defer close(ch)
		if len(msg) == 0 || msg == nil {
			s.Warning("peer %v responded with nil to atx %v request", peer, epoch)
			return
		}
		var atxIDs []types.ATXID
		err := types.BytesToInterface(msg, &atxIDs)
		if err != nil {
			s.Error("could not unmarshal mesh.LayerIDs response ", err)
			return
		}
		ch <- atxIDs
	}
	if err := s.SendRequest(atxIdsMsg, epoch.ToBytes(), peer, foo, func(err error) {}); err != nil {
		return nil, err
	}
	return ch, nil
}

func hashReqFactory(lyr types.LayerID) requestFactory {
	return func(s networker, peer p2ppeers.Peer) (chan interface{}, error) {
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
		if err := s.SendRequest(layerHashMsg, lyr.Bytes(), peer, foo, func(err error) {}); err != nil {
			return nil, err
		}

		return ch, nil
	}

}

func atxHashReqFactory(ep types.EpochID) requestFactory {
	return func(s networker, peer p2ppeers.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to hash request layer %v", peer, ep)
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
		if err := s.SendRequest(atxIdrHashMsg, ep.ToBytes(), peer, foo, func(err error) {}); err != nil {
			return nil, err
		}

		return ch, nil
	}

}

func newFetchReqFactory(msgtype server.MessageType, asItems func(msg []byte) ([]item, error)) batchRequestFactory {
	//convert to chan
	return func(infra networker, peer p2ppeers.Peer, ids []types.Hash32) (chan []item, error) {
		ch := make(chan []item, 1)
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

func atxsAsItems(msg []byte) ([]item, error) {
	var atxs []types.ActivationTx
	err := types.BytesToInterface(msg, &atxs)
	if err != nil || atxs == nil {
		return nil, err
	}
	atxs = calcAndSetIds(atxs)
	items := make([]item, len(atxs))
	for i := range atxs {
		items[i] = &atxs[i]
	}
	return items, nil
}

func txsAsItems(msg []byte) ([]item, error) {
	var txs []*types.Transaction
	err := types.BytesToInterface(msg, &txs)
	if err != nil || txs == nil {
		return nil, err
	}
	items := make([]item, len(txs))
	for i := range txs {
		err := txs[i].CalcAndSetOrigin()
		if err != nil {
			return nil, fmt.Errorf("failed to calc transaction origin (id: %s): %v",
				txs[i].ID().ShortString(), err)
		}
		items[i] = txs[i]
	}
	return items, nil
}

func blocksAsItems(msg []byte) ([]item, error) {
	var blocks []types.Block
	err := types.BytesToInterface(msg, &blocks)
	if err != nil {
		return nil, err
	}
	items := make([]item, len(blocks))
	for i := range blocks {
		blocks[i].Initialize()
		items[i] = &blocks[i]
	}
	return items, nil
}

func calcAndSetIds(atxs []types.ActivationTx) []types.ActivationTx {
	atxsWithIds := make([]types.ActivationTx, 0, len(atxs))
	for _, atx := range atxs {
		atx.CalcAndSetID()
		atxsWithIds = append(atxsWithIds, atx)
	}
	return atxsWithIds
}

func poetReqFactory(poetProofRef []byte) requestFactory {
	return func(s networker, peer p2ppeers.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		resHandler := func(msg []byte) {
			s.Info("handle PoET proof response")
			defer close(ch)
			if len(msg) == 0 || msg == nil {
				s.Warning("peer %v responded with nil to poet request %v", peer, poetProofRef)
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

		if err := s.SendRequest(poetMsg, poetProofRef, peer, resHandler, func(err error) {}); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func validatePoetRef(proofMessage types.PoetProofMessage, poetProofRef []byte) (bool, error) {
	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		return false, fmt.Errorf("could marshal PoET response for validation %v", err)
	}
	b := sha256.Sum256(poetProofBytes)
	if bytes.Compare(types.CalcHash32(b[:]).Bytes(), poetProofRef) != 0 {
		return false, fmt.Errorf("poet recived was diffrent then requested")
	}

	return true, nil
}

func validateItemIds(ids []types.Hash32, items []item) (bool, error) {
	mp := make(map[types.Hash32]struct{})
	for _, id := range ids {
		mp[id] = struct{}{}
	}
	for _, tx := range items {
		txid := tx.Hash32()
		if _, ok := mp[txid]; !ok {
			return false, fmt.Errorf("received item that was not requested %v type %v", tx.ShortString(), reflect.TypeOf(tx))
		}
		delete(mp, txid)
	}

	if len(mp) > 0 {
		for id := range mp {
			log.Warning("item %s was not in response ", id.ShortString())
		}
	}

	return true, nil
}

func encodeAndSendRequest(req server.MessageType, ids []types.Hash32, s networker, peer p2ppeers.Peer, foo func(msg []byte)) error {
	bts, err := types.InterfaceToBytes(ids)
	if err != nil {
		return err
	}
	if err := s.SendRequest(req, bts, peer, foo, func(err error) {}); err != nil {
		return err
	}
	return nil
}
