package sync

/*
import (
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
	"time"
)

// BlockListener Listens to blocks propagated in gossip
type BlockListener struct {
	blocks.BlockEligibilityValidator
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
	// this stops us from receiving any new blocks, but we may still already be processing received blocks
	close(bl.exit)
	bl.Info("block listener closing, waiting for goroutines to finish")
	// this waits for processing of received blocks to finish
	bl.wg.Wait()
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
	bl.wg.Add(1)
	defer bl.wg.Done()
	for {
		select {
		case <-bl.exit:
			bl.Log.Info("stopped listening to gossip blocks")
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
}*/
