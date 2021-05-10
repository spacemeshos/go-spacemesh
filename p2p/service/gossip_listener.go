package service

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"sync"
)

// GossipDataHandler is the function type that will be called when data is
type GossipDataHandler func(ctx context.Context, data GossipMessage, syncer Fetcher)

// Listener represents the main struct that reqisters delegates to gossip function
type Listener struct {
	*log.Log
	net      Service
	channels []chan GossipMessage
	stoppers []chan struct{}
	syncer   Fetcher
	wg       sync.WaitGroup
}

// NewListener creates a new listener struct
func NewListener(net Service, syncer Fetcher, log log.Log) *Listener {
	return &Listener{
		Log:    &log,
		net:    net,
		syncer: syncer,
		wg:     sync.WaitGroup{},
	}
}

// Syncer is interface for sync services
type Syncer interface {
	FetchAtxReferences(context.Context, *types.ActivationTx) error
	FetchPoetProof(ctx context.Context, poetProofRef []byte) error
	ListenToGossip() bool
	GetBlock(ID types.BlockID) (*types.Block, error)
	//GetTxs(IDs []types.TransactionID) error
	//GetBlocks(IDs []types.BlockID) error
	//GetAtxs(IDs []types.ATXID) error
	IsSynced(context.Context) bool
}

// Fetcher is a general interface that defines a component capable of fetching data from remote peers
type Fetcher interface {
	FetchBlock(context.Context, types.BlockID) error
	FetchAtx(context.Context, types.ATXID) error
	GetPoetProof(context.Context, types.Hash32) error
	GetTxs(context.Context, []types.TransactionID) error
	GetBlocks(context.Context, []types.BlockID) error
	GetAtxs(context.Context, []types.ATXID) error
	ListenToGossip() bool
	IsSynced(context.Context) bool
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
	l.WithContext(ctx).Info("start listening to gossip", log.String("protocol", channel))
	for {
		select {
		case <-stop:
			l.wg.Done()
			return
		case data := <-gossipChannel:
			l.WithContext(ctx).With().Info("got gossip message, forwarding to data handler",
				log.String("protocol", channel),
				log.Int("queue_length", len(gossipChannel)))
			if !l.syncer.ListenToGossip() {
				// not accepting data
				continue
			}
			dataHandler(log.WithNewRequestID(ctx), data, l.syncer)
		}
	}
}
