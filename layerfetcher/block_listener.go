package layerfetcher

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	sync2 "github.com/spacemeshos/go-spacemesh/sync"
	"sync"
	"time"
)

// BlockListener Listens to blocks propagated in gossip
type BlockListener struct {
	*sync2.Syncer
	sync2.blockEligibilityValidator
	log.Log
	wg                   sync.WaitGroup
	bufferSize           int
	semaphore            chan struct{}
	receivedGossipBlocks chan service.GossipMessage
	startLock            types.TryMutex
	timeout              time.Duration
	exit                 chan struct{}
}

// Close closes all running goroutines
func (bl *BlockListener) Close() {
	close(bl.exit)
	bl.Info("block listener closing, waiting for gorutines")
	bl.wg.Wait()
	bl.Syncer.Close()
	bl.Info("block listener closed")
}

// Start starts the main listening goroutine
func (bl *BlockListener) Start() {
	if bl.startLock.TryLock() {
		go bl.listenToGossipBlocks()
	}
}

// NewBlockListener creates a new instance of BlockListener
func NewBlockListener(net service.Service, sync *sync2.Syncer, concurrency int, logger log.Log) *BlockListener {
	bl := BlockListener{
		Syncer:               sync,
		Log:                  logger,
		semaphore:            make(chan struct{}, concurrency),
		exit:                 make(chan struct{}),
		receivedGossipBlocks: net.RegisterGossipProtocol(config.NewBlockProtocol, priorityq.High),
	}
	return &bl
}

func (bl *BlockListener) listenToGossipBlocks() {
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("listening  stopped")
			return
		case data := <-bl.receivedGossipBlocks:
			if !bl.ListenToGossip() {
				bl.With().Info("ignoring gossip blocks - not synced yet")
				break
			}

			bl.wg.Add(1)
			go func() {
				defer bl.wg.Done()
				if data == nil {
					bl.Error("got empty message while listening to gossip blocks")
					return
				}
				tmr := sync2.newMilliTimer(sync2.gossipBlockTime)
				bl.handleBlock(data)
				tmr.ObserveDuration()
			}()

		}
	}
}


