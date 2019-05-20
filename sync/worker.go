package sync

import (
	"encoding/hex"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type worker struct {
	sync.Once
	count    *int32
	teardown func()
	action   func()
	log.Log
}

func (w *worker) Work() {
	w.action()
	atomic.AddInt32(w.count, -1)
	if atomic.LoadInt32(w.count) == 0 { //close once everyone is finished
		w.Debug("worker teardown")
		w.Do(w.teardown)
	}
}

func NewLayerIdsWorker(s *Syncer, lyr types.LayerID, peer p2p.Peer, output chan []types.BlockID, count *int32) worker {

	foo := func() {
		log.Debug("send blockIds request Peer: %v layer:  %v", peer, lyr)
		ch, foo := layerBlockIDsRequest()
		if err := s.SendRequest(LAYER_IDS, lyr.ToBytes(), peer, foo); err != nil {
			s.Error("could not get layer ", lyr, " hash from peer ", peer)
			return
		}
		timeout := time.After(s.RequestTimeout)
		select {
		case <-timeout:
			s.Error("layer ids request to %v timed out", peer)
			return
		case v := <-ch:
			output <- v
		}
	}

	return worker{
		Log:      s.Log,
		count:    count,
		teardown: func() { close(output) },
		action:   foo,
	}

}

func NewHashWorker(s *Syncer, lyr types.LayerID, peer p2p.Peer, output chan *peerHashPair, count *int32) worker {
	foo := func() {
		log.Info("send layer hash request Peer: %v layer: %v", peer, lyr)
		c, foo := layerHashRequest(peer, lyr)
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, foo); err != nil {
			s.Error("could not get layer ", lyr, " hash from peer ", peer)
			return
		}

		timeout := time.After(s.RequestTimeout)
		select {
		case <-timeout:
			s.Error("hash request to %v timed out", peer)
			return
		case v := <-c:
			output <- v
		}
	}
	return worker{
		Log:      s.Log,
		count:    count,
		teardown: func() { close(output) },
		action:   foo,
	}

}

//todo handle blocks in retry queue
func NewBlockWorker(s *Syncer, blockIds chan types.BlockID, retry chan types.BlockID, output chan *types.MiniBlock, count *int32) worker {

	foo := func() {
		for id := range blockIds {
			//todo check peers not empty
			for _, p := range s.GetPeers() {
				timer := newMilliTimer(blockTime)
				log.Info("send block request Peer: %v id: %v", p, id)
				bCh, foo := miniBlockRequest()
				if err := s.SendRequest(BLOCK, id.ToBytes(), p, foo); err == nil {
					timeout := time.After(s.RequestTimeout)
					select {
					case <-timeout:
						s.Error("block request to %v timed out move to retry queue", id)
						retry <- id
					case block := <-bCh:
						if block != nil {
							elapsed := timer.ObserveDuration()
							s.Info("fetching block %v took %v ", block.ID(), elapsed)
							blockCount.Add(1)
							if eligible, err := s.BlockEligible(&block.BlockHeader); err != nil {
								s.Error("block eligibility check failed: %v", err) //todo leave block out of layer ?
							} else if eligible { //some validation testing
								output <- block
							}
						} else {
							s.Info("fetching block %d failed move to retry queue", id)
							retry <- id
						}
					}
				}
			}
		}
	}

	return worker{
		Log:      s.Log,
		count:    count,
		teardown: func() { close(output) },
		action:   foo,
	}
}

//todo batch requests
func NewTxWorker(s *Syncer, txIds []types.TransactionId, retry chan types.TransactionId, output chan *types.SerializableTransaction, count *int32) worker {

	foo := func() {
		for _, id := range txIds {
			//todo check peers not empty
			for _, p := range s.GetPeers() {
				timer := newMilliTimer(txTime)
				log.Info("send tx request to Peer: %v id: %v", p, hex.EncodeToString(id[:]))
				tCh, foo := txRequest()
				if err := s.SendRequest(TX, id[:], p, foo); err == nil {
					timeout := time.After(s.RequestTimeout)
					select {
					case <-timeout:
						s.Error("tx request to %v timed out move to retry queue", hex.EncodeToString(id[:]))
						retry <- id
					case tx := <-tCh:
						if tx != nil {
							elapsed := timer.ObserveDuration()
							s.Info("fetching tx %v took %v ", hex.EncodeToString(id[:]), elapsed)
							txCount.Add(1)
							output <- tx
							s.Info("after chan")
						} else {
							s.Info("fetching block %v failed move to retry queue", hex.EncodeToString(id[:]))
							retry <- id
						}
					}
				}
			}
		}
	}

	return worker{
		Log:      s.Log,
		count:    count,
		teardown: func() { close(output) },
		action:   foo,
	}
}
