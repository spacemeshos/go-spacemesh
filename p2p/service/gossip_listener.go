package service

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
)

// GossipDataHandler is the function type that will be called when data is
type GossipDataHandler func(data GossipMessage, syncer Syncer)

// Listener represents the main struct that reqisters delegates to gossip function
type Listener struct {
	*log.Log
	net      Service
	channels []chan GossipMessage
	stoppers []chan struct{}
	syncer   Syncer
	wg       sync.WaitGroup
}

// NewListener creates a new listener struct
func NewListener(net Service, syncer Syncer, log log.Log) *Listener {
	return &Listener{
		Log:    &log,
		net:    net,
		syncer: syncer,
		wg:     sync.WaitGroup{},
	}
}

// Syncer is interface for sync services
type Syncer interface {
	FetchPoetProof(poetProofRef []byte) error
	ListenToGossip() bool
	IsSynced() bool
}

// AddListener adds a listener to a specific gossip channel
func (l *Listener) AddListener(channel string, priority priorityq.Priority, dataHandler GossipDataHandler) {
	ch := l.net.RegisterGossipProtocol(channel, priority)
	stop := make(chan struct{})
	l.channels = append(l.channels, ch)
	l.stoppers = append(l.stoppers, stop)
	l.wg.Add(1)
	go l.listenToGossip(dataHandler, ch, stop)
}

// Stop stops listening to all gossip channels
func (l *Listener) Stop() {
	for _, ch := range l.stoppers {
		close(ch)
	}
	l.wg.Wait()
}

func (l *Listener) listenToGossip(dataHandler GossipDataHandler, gossipChannel chan GossipMessage, stop chan struct{}) {
	l.Info("start listening")
	for {
		select {
		case <-stop:
			l.wg.Done()
			return
		case data := <-gossipChannel:
			if !l.syncer.ListenToGossip() {
				// not accepting data
				continue
			}
			dataHandler(data, l.syncer)
		}
	}
}
