package sync

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/log"
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
	close(blockIdsCh)
	return blockIdsCh
}

func BlockReqFactory() BlockRequestFactory {
	//convert to chan
	return func(s *server.MessageServer, peer p2p.Peer, id types.BlockID) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
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
func TxReqFactory(ids []types.TransactionId, sync *Syncer) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
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

func ATxReqFactory(ids []types.AtxId, syncer *Syncer, blkId types.BlockID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		atxstring := ""
		for _, i := range ids {
			atxstring += i.ShortId() + ", "
		}
		s.With().Info("about to sync atxs with peer", log.String("atx_list", atxstring), log.BlockId(uint64(blkId)))
		foo := func(msg []byte) {
			defer close(ch)
			var tx []types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			atxstring := ""
			for _, i := range tx {
				atxstring += i.ShortId() + ", "
			}
			s.Info("handle atx response ", log.String("atx_list", atxstring), log.BlockId(uint64(blkId)))

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

func PoetReqFactory(poetProofRef []byte, syncer *Syncer) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		if pr, err := syncer.poetDb.GetProofMessage(poetProofRef); err == nil {
			s.With().Info("found poet ref in local db, stop fetching from neighbors",
				log.String("poet_ref", hex.EncodeToString(poetProofRef[:6])))
			var proofMessage types.PoetProofMessage
			err := types.BytesToInterface(pr, &proofMessage)
			if err != nil {
				s.With().Error("could not unmarshal PoET proof message, continue asking peers",
					log.String("poet_ref", hex.EncodeToString(poetProofRef[:6])), log.Err(err))
			} else {
				ch <- proofMessage
				close(ch)
				return ch, nil
			}
		}
		s.With().Info("couldn't find poet ref in local db, fetching from neighbors",
			log.String("poet_ref", hex.EncodeToString(poetProofRef[:6])))
		resHandler := func(msg []byte) {
			s.Info("handle PoET proof response")
			defer close(ch)
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
