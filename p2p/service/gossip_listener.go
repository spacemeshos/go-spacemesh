package service

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
)

type GossipDataHandler func(data GossipMessage, syncer Syncer)

type Listener struct {
	*log.Log
	net      Service
	channels []chan GossipMessage
	stoppers []chan struct{}
	syncer   Syncer
	wg sync.WaitGroup
}

func NewListener(net Service, syncer Syncer, log log.Log) *Listener {
	return &Listener{
	Log: &log,
	net: net,
	syncer:syncer,
	wg:sync.WaitGroup{},
	}
}

type Syncer interface {
	FetchPoetProof(poetProofRef []byte) error
	ListenToGossip() bool
	IsSynced() bool
}

func (l *Listener) AddListener(channel string, priority priorityq.Priority, dataHandler GossipDataHandler) {
	ch := l.net.RegisterGossipProtocol(channel, priority)
	stop := make(chan struct{})
	l.channels = append(l.channels, ch)
	l.stoppers = append(l.stoppers, stop)
	l.wg.Add(1)
	go l.listenToGossip(dataHandler, ch, stop)
}

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
