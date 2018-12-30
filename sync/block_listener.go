package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"sync/atomic"
	"time"
)

type MessageServer server.MessageServer

const BufferSize = 200 //todo tune buffer to correct size

const blockProtocol = "/blocks/1.0/"

type BlockListener struct {
	*server.MessageServer
	Peers
	mesh.Mesh
	BlockValidator
	workers      chan struct{}     //semaphore
	unknownQueue chan mesh.BlockID //todo consider benefits of changing to stack
	startLock    uint32
	timeout      time.Duration
	exit         chan struct {
	}
}

func (bl *BlockListener) Close() {
	close(bl.exit)
}

func (bl *BlockListener) Start() {
	if atomic.CompareAndSwapUint32(&bl.startLock, 0, 1) {
		go bl.run()
	}
}

func NewBlockListener(peers Peers, bv BlockValidator, layers mesh.Mesh, timeout time.Duration, concurrency int) *BlockListener {
	bl := BlockListener{
		MessageServer:  server.NewMsgServer(peers, blockProtocol, timeout),
		Mesh:           layers,
		Peers:          peers,
		BlockValidator: bv,
		workers:        make(chan struct{}, concurrency),
		unknownQueue:   make(chan mesh.BlockID, BufferSize),
		exit:           make(chan struct{})}
	bl.RegisterMsgHandler(BLOCK, newBlockRequestHandler(layers))
	return &bl
}

//todo limit concurrency
func (bl *BlockListener) run() {
	for {
		select {
		case <-bl.exit:
			log.Debug("run stoped")
			return
		case id := <-bl.unknownQueue:
			log.Debug("fetch block ", id)
			bl.workers <- struct{}{}
			go func() {
				defer func() { <-bl.workers }()
				bl.FetchBlock(id)
			}()
		}
	}
}

//todo handle case where no peer knows the block
func (bl *BlockListener) FetchBlock(id mesh.BlockID) {
	for _, p := range bl.GetPeers() {
		ch, err := sendBlockRequest(bl.MessageServer, p, id)
		if err == nil {
			if b := <-ch; b != nil {
				if bl.ValidateBlock(b) {
					bl.AddBlock(b)
					//add all child blocks to unknown queue
					for block := range b.BlockVotes {
						//if unknown block
						if _, err := bl.GetBlock(block); err != nil {
							bl.unknownQueue <- block
						}
					}
				}
			}
		}
	}
}
