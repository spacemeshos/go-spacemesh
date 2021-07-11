// Package gossip implements simple protocol to send new validated messages to all peers and ignore old or not valid messages.
package gossip

import (
	"context"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

const oldMessageCacheSize = 10000
const propagateHandleBufferSize = 5000 // number of MessageValidation that we allow buffering, above this number protocols will get stuck

type peersManager interface {
	GetPeers() []peers.Peer
	PeerCount() uint64
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(ctx context.Context, peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessGossipProtocolMessage(ctx context.Context, sender p2pcrypto.PublicKey, ownMessage bool, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error
}

// Protocol runs the gossip protocol using the given peers and service.
type Protocol struct {
	log.Log

	config          config.SwarmConfig
	net             baseNetwork
	localNodePubkey p2pcrypto.PublicKey

	peers peersManager

	shutdownCtx context.Context

	oldMessageQ *types.DoubleCache

	propagateQ chan service.MessageValidation
	pq         priorityq.PriorityQueue
	priorities map[string]priorityq.Priority
}

// NewProtocol creates a new gossip protocol instance.
func NewProtocol(ctx context.Context, config config.SwarmConfig, base baseNetwork, peersManager peersManager, localNodePubkey p2pcrypto.PublicKey, logger log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	return &Protocol{
		Log:             logger,
		config:          config,
		net:             base,
		localNodePubkey: localNodePubkey,
		peers:           peersManager,
		shutdownCtx:     ctx,
		oldMessageQ:     types.NewDoubleCache(oldMessageCacheSize), // todo : remember to drain this
		propagateQ:      make(chan service.MessageValidation, propagateHandleBufferSize),
		pq:              priorityq.New(propagateHandleBufferSize),
		priorities:      make(map[string]priorityq.Priority),
	}
}

// Start a loop that process peers events
func (p *Protocol) Start(ctx context.Context) {
	go p.propagationEventLoop(ctx) // TODO consider running several consumers
}

// Broadcast is the actual broadcast procedure - process the message internally and loop on peers and add the message to their queues
func (p *Protocol) Broadcast(ctx context.Context, payload []byte, nextProt string) error {
	p.WithContext(ctx).With().Debug("broadcasting message", log.String("from_type", nextProt))
	return p.processMessage(ctx, p.localNodePubkey, true, nextProt, service.DataBytes{Payload: payload})
	//todo: should this ever return error ? then when processMessage should return error ?. should it block?
}

// Relay processes a message, if the message is new, it is passed for the protocol to validate and then propagated.
func (p *Protocol) Relay(ctx context.Context, sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	p.WithContext(ctx).With().Debug("relaying message", log.String("from_type", protocol))
	return p.processMessage(ctx, sender, false, protocol, msg)
}

// SetPriority sets the priority for protoName in the queue.
func (p *Protocol) SetPriority(protoName string, priority priorityq.Priority) {
	p.priorities[protoName] = priority
}

// markMessageAsOld adds the message's hash to the old messages queue so that the message won't be processed in case received again.
// Returns true if message was already processed before
func (p *Protocol) markMessageAsOld(h types.Hash12) bool {
	return p.oldMessageQ.GetOrInsert(h)
}

func (p *Protocol) processMessage(ctx context.Context, sender p2pcrypto.PublicKey, ownMessage bool, protocol string, msg service.Data) error {
	h := types.CalcMessageHash12(msg.Bytes(), protocol)
	logger := p.WithContext(ctx).WithFields(
		log.FieldNamed("msg_sender", sender),
		log.String("protocol", protocol),
		log.String("msghash", util.Bytes2Hex(h[:])))
	logger.Debug("checking gossip message newness")
	if p.markMessageAsOld(h) {
		metrics.OldGossipMessages.With(metrics.ProtocolLabel, protocol).Add(1)
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer we got this message already?
		// todo : - maybe block this peer since he sends us old messages
		logger.Debug("gossip message is old, dropping")
		return nil
	}

	logger.Event().Debug("gossip message is new, processing")
	metrics.NewGossipMessages.With("protocol", protocol).Add(1)
	return p.net.ProcessGossipProtocolMessage(ctx, sender, ownMessage, protocol, msg, p.propagateQ)
}

// send a message to all the peers.
func (p *Protocol) propagateMessage(ctx context.Context, payload []byte, nextProt string, exclude p2pcrypto.PublicKey) {
	//TODO soon: don't wait for message to send and if we finished sending last message one of the peers send the next
	// message. limit the number of simultaneous sends. consider other messages (mainly sync).
	var wg sync.WaitGroup
peerLoop:
	for _, peer := range p.peers.GetPeers() {
		if exclude == peer {
			continue peerLoop
		}
		wg.Add(1)
		go func(pubkey p2pcrypto.PublicKey) {
			// TODO: replace peer ?

			// Add recipient to context for logs
			msgCtx := ctx
			if reqID, ok := log.ExtractRequestID(ctx); ok {
				// overwrite the existing reqID with the same and add the field
				// (there is currently no easier way to add a field to log ctx)
				msgCtx = log.WithRequestID(ctx, reqID, log.FieldNamed("to_id", pubkey))
			}

			p.WithContext(msgCtx).Debug("propagating gossip message to peer")
			if err := p.net.SendMessage(msgCtx, pubkey, nextProt, payload); err != nil {
				p.WithContext(msgCtx).With().Warning("failed sending", log.Err(err))
			}
			wg.Done()
		}(peer)
	}
	wg.Wait()
}

func (p *Protocol) handlePQ(ctx context.Context) {
	for {
		if p.isShuttingDown() {
			return
		}
		mi, err := p.pq.Read()
		if err != nil {
			p.WithContext(ctx).With().Info("priority queue was closed, exiting", log.Err(err))
			return
		}
		m, ok := mi.(service.MessageValidation)
		if !ok {
			p.WithContext(ctx).Error("could not convert to message validation, ignoring message")
			continue
		}
		// read message requestID
		h := types.CalcMessageHash12(m.Message(), m.Protocol())
		extraFields := []log.LoggableField{
			h,
			log.FieldNamed("msg_sender", m.Sender()),
			log.String("protocol", m.Protocol()),
		}
		var msgCtx context.Context
		if m.RequestID() == "" {
			msgCtx = log.WithNewRequestID(ctx, extraFields...)
			p.WithContext(msgCtx).Warning("message in queue has no requestId, generated new requestId")
		} else {
			msgCtx = log.WithRequestID(ctx, m.RequestID(), extraFields...)
		}
		p.WithContext(msgCtx).With().Debug("new_gossip_message_relay",
			log.Int("priority_queue_length", p.pq.Length()))
		if p.pq.Length() > 50 {
			p.WithContext(msgCtx).With().Warning("outbound gossip message queue backlog",
				log.Int("priority_queue_length", p.pq.Length()))
		}
		p.propagateMessage(msgCtx, m.Message(), m.Protocol(), m.Sender())
		p.WithContext(msgCtx).With().Debug("finished propagating gossip message")
	}
}

func (p *Protocol) getPriority(protoName string) priorityq.Priority {
	v, exist := p.priorities[protoName]
	if !exist {
		p.With().Warning("no priority found for protocol",
			log.String("protoName", protoName))
		return priorityq.Low
	}
	return v
}

func (p *Protocol) isShuttingDown() bool {
	select {
	case <-p.shutdownCtx.Done():
		return true
	default:
	}
	return false
}

// pushes messages that passed validation into the priority queue
func (p *Protocol) propagationEventLoop(ctx context.Context) {
	go p.handlePQ(ctx)

	for {
		select {
		case msgV := <-p.propagateQ:
			if p.isShuttingDown() {
				return
			}
			// Note: this will block iff the priority queue is full
			if err := p.pq.Write(p.getPriority(msgV.Protocol()), msgV); err != nil {
				p.WithContext(ctx).With().Error("could not write to priority queue",
					log.Err(err),
					log.String("protocol", msgV.Protocol()))
			}
			p.WithContext(ctx).With().Debug("wrote inbound message to priority queue",
				log.String("protocol", msgV.Protocol()),
				log.Int("priority_queue_length", p.pq.Length()),
				log.Int("propagation_queue_length", len(p.propagateQ)))
			metrics.PropagationQueueLen.Set(float64(len(p.propagateQ)))

		case <-p.shutdownCtx.Done():
			p.pq.Close()
			p.WithContext(ctx).Error("propagate event loop stopped: protocol shutdown")
			return
		}
	}
}
