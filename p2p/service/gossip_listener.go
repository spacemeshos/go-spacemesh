package service

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

type GossipDataHandler func(data GossipMessage, syncer Syncer)

type Listener struct {
	*log.Log
	net      Service
	channels []chan GossipMessage
	stoppers []chan struct{}
	syncer   Syncer
}

func NewListener(net Service, syncer Syncer, log log.Log) *Listener {
	return &Listener{
	Log: &log,
	net: net,
	syncer:syncer,
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
	go l.listenToGossip(dataHandler, ch, stop)
}

func (l *Listener) Stop() {
	for _, ch := range l.stoppers {
		close(ch)
	}
}

func (l *Listener) listenToGossip(dataHandler GossipDataHandler, gossipChannel chan GossipMessage, stop chan struct{}) {
	l.Info("start listening")
	for {
		select {
		case <-stop:
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
