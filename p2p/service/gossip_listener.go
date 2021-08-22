package service

import (
	"context"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

// GossipDataHandler is the function type that will be called when data is
type GossipDataHandler func(ctx context.Context, data GossipMessage, fetcher Fetcher)

type enableGossipFunc func() bool

// Listener represents the main struct that reqisters delegates to gossip function
type Listener struct {
	*log.Log
	net                  Service
	channels             []chan GossipMessage
	stoppers             []chan struct{}
	fetcher              Fetcher
	wg                   sync.WaitGroup
	shouldListenToGossip enableGossipFunc
	config               config.Config
}

// NewListener creates a new listener struct
func NewListener(net Service, fetcher Fetcher, shouldListenToGossip enableGossipFunc, config config.Config, log log.Log) *Listener {
	return &Listener{
		Log:                  &log,
		net:                  net,
		fetcher:              fetcher,
		shouldListenToGossip: shouldListenToGossip,
		wg:                   sync.WaitGroup{},
		config:               config,
	}
}

// Fetcher is a general interface that defines a component capable of fetching data from remote peers
type Fetcher interface {
	FetchBlock(context.Context, types.BlockID) error
	FetchAtx(context.Context, types.ATXID) error
	GetPoetProof(context.Context, types.Hash32) error
	GetTxs(context.Context, []types.TransactionID) error
	GetBlocks(context.Context, []types.BlockID) error
	GetAtxs(context.Context, []types.ATXID) error
}

// AddListener adds a listener to a specific gossip channel
func (l *Listener) AddListener(ctx context.Context, channel string, priority priorityq.Priority, dataHandler GossipDataHandler) {
	ctx = log.WithNewSessionID(ctx, log.String("channel", channel))
	ch := l.net.RegisterGossipProtocol(channel, priority)
	stop := make(chan struct{})
	l.channels = append(l.channels, ch)
	l.stoppers = append(l.stoppers, stop)
	l.wg.Add(1)
	go l.listenToGossip(log.WithNewSessionID(ctx), dataHandler, ch, stop, channel)
}

// Stop stops listening to all gossip channels
func (l *Listener) Stop() {
	for _, ch := range l.stoppers {
		close(ch)
	}
	l.wg.Wait()
}

func (l *Listener) listenToGossip(ctx context.Context, dataHandler GossipDataHandler, gossipChannel chan GossipMessage, stop chan struct{}, channel string) {
	l.WithContext(ctx).With().Info("start listening to gossip", log.String("protocol", channel))

	// fill the channel with tokens to limit number of concurrent routines
	tokenChan := make(chan struct{}, l.config.MaxGossipRoutines)
	for i := 0; i < l.config.MaxGossipRoutines; i++ {
		tokenChan <- struct{}{}
	}

	handleMsg := func(ctx context.Context, data GossipMessage) {
		// get a token to create a new channel
		l.WithContext(ctx).With().Info("waiting for available slot for gossip handler",
			log.Int("available_slots", len(tokenChan)),
			log.Int("total_slots", cap(tokenChan)))
		if len(tokenChan) == 0 {
			l.WithContext(ctx).Error("no available slots for gossip handler, blocking")
		}
		<-tokenChan

		l.WithContext(ctx).With().Info("got gossip message, forwarding to data handler",
			log.String("protocol", channel),
			log.Int("queue_length", len(gossipChannel)))
		if !l.shouldListenToGossip() {
			// not accepting data
			l.WithContext(ctx).Info("not currently listening to gossip, dropping message")
			return
		}
		go func() {
			// TODO: these handlers should have an API that includes a cancel method. they should time out eventually.
			dataHandler(ctx, data, l.fetcher)
			// replace token when done
			tokenChan <- struct{}{}
		}()
	}

	for {
		select {
		case <-stop:
			l.wg.Done()
			return
		case data := <-gossipChannel:
			l.WithContext(ctx).With().Debug("got gossip message, forwarding to data handler",
				log.String("protocol", channel),
				log.Int("queue_length", len(gossipChannel)))
			if !l.shouldListenToGossip() {
				// not accepting data
				continue
			}
			handleMsg(log.WithNewRequestID(ctx), data)
		}
	}
}
