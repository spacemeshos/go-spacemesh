// Package gossip implements simple protocol to send new validated messages to all peers and ignore old or not valid messages.
package gossip

import (
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

//go:generate mockgen -package=gossip -destination=./protocol_mock_test.go -source=./protocol.go peersManager, baseNetwork, prioQ

const oldMessageCacheSize = 10000
const propagateHandleBufferSize = 5000 // number of MessageValidation that we allow buffering, above this number protocols will get stuck

type peersManager interface {
	GetPeers() []peers.Peer
	PeerCount() uint64
}

// Interface for the underlying p2p layer
type baseNetwork interface {
	SendMessage(peerPubkey p2pcrypto.PublicKey, protocol string, payload []byte) error
	SubscribePeerEvents() (conn chan p2pcrypto.PublicKey, disc chan p2pcrypto.PublicKey)
	ProcessGossipProtocolMessage(sender p2pcrypto.PublicKey, protocol string, data service.Data, validationCompletedChan chan service.MessageValidation) error
}

type prioQ interface {
	Write(prio priorityq.Priority, m interface{}) error
	Read() (interface{}, error)
	Close()
}

// Protocol runs the gossip protocol using the given peers and service.
type Protocol struct {
	log.Log

	config          config.SwarmConfig
	net             baseNetwork
	localNodePubkey p2pcrypto.PublicKey

	peers peersManager

	shutdown chan struct{}

	oldMessageQ *types.DoubleCache

	propagateQ chan service.MessageValidation
	pq         prioQ
	priorities map[string]priorityq.Priority
}

// NewProtocol creates a new gossip protocol instance.
func NewProtocol(config config.SwarmConfig, base baseNetwork, peersManager peersManager, localNodePubkey p2pcrypto.PublicKey, logger log.Log) *Protocol {
	// intentionally not subscribing to peers events so that the channels won't block in case executing Start delays
	return &Protocol{
		Log:             logger,
		config:          config,
		net:             base,
		localNodePubkey: localNodePubkey,
		peers:           peersManager,
		shutdown:        make(chan struct{}),
		oldMessageQ:     types.NewDoubleCache(oldMessageCacheSize), // todo : remember to drain this
		propagateQ:      make(chan service.MessageValidation, propagateHandleBufferSize),
		pq:              priorityq.New(propagateHandleBufferSize),
		priorities:      make(map[string]priorityq.Priority),
	}
}

// Start a loop that process peers events
func (p *Protocol) Start() {
	go p.propagationEventLoop() // TODO consider running several consumers
}

// Close stops all protocol routines.
func (p *Protocol) Close() {
	close(p.shutdown)
}

// Broadcast is the actual broadcast procedure - process the message internally and loop on peers and add the message to their queues
func (p *Protocol) Broadcast(payload []byte, nextProt string) error {
	p.Log.Debug("Broadcasting message from type %s", nextProt)
	return p.processMessage(p.localNodePubkey, nextProt, service.DataBytes{Payload: payload})
	//todo: should this ever return error ? then when processMessage should return error ?. should it block?
}

// Relay processes a message, if the message is new, it is passed for the protocol to validate and then propagated.
func (p *Protocol) Relay(sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	return p.processMessage(sender, protocol, msg)
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

func (p *Protocol) processMessage(sender p2pcrypto.PublicKey, protocol string, msg service.Data) error {
	h := types.CalcMessageHash12(msg.Bytes(), protocol)
	if p.markMessageAsOld(h) {
		metrics.OldGossipMessages.With(metrics.ProtocolLabel, protocol).Add(1)
		// todo : - have some more metrics for termination
		// todo	: - maybe tell the peer we got this message already?
		// todo : - maybe block this peer since he sends us old messages
		p.Log.With().Debug("old_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", util.Bytes2Hex(h[:])))
		return nil
	}

	p.Log.Event().Debug("new_gossip_message", log.String("from", sender.String()), log.String("protocol", protocol), log.String("hash", util.Bytes2Hex(h[:])))
	metrics.NewGossipMessages.With("protocol", protocol).Add(1)
	return p.net.ProcessGossipProtocolMessage(sender, protocol, msg, p.propagateQ)
}

// send a message to all the peers.
func (p *Protocol) propagateMessage(payload []byte, h types.Hash12, nextProt string, exclude p2pcrypto.PublicKey) {
	//TODO soon : don't wait for mesaage to send and if we finished sending last message one of the peers send the next message to him.
	// limit the number of simultaneous sends. *consider other messages (mainly sync)
	var wg sync.WaitGroup
peerLoop:
	for _, peer := range p.peers.GetPeers() {
		if exclude == peer {
			continue peerLoop
		}
		wg.Add(1)
		go func(pubkey p2pcrypto.PublicKey) {
			// TODO: replace peer ?
			if err := p.net.SendMessage(pubkey, nextProt, payload); err != nil {
				p.With().Warning("Failed sending", log.String("protocol", nextProt), h.Field("hash"), log.String("to", pubkey.String()), log.Err(err))
			}
			wg.Done()
		}(peer)
	}
	wg.Wait()
}

func (p *Protocol) handlePQ() {
	for {
		mi, err := p.pq.Read()
		if err != nil {
			p.With().Info("priority queue was closed, existing", log.Err(err))
			return
		}
		m, ok := mi.(service.MessageValidation)
		if !ok {
			p.Error("could not convert to message validation, ignoring message")
			continue
		}
		h := types.CalcMessageHash12(m.Message(), m.Protocol())
		p.Log.With().Debug("new_gossip_message_relay", log.String("protocol", m.Protocol()), log.String("hash", util.Bytes2Hex(h[:])))
		p.propagateMessage(m.Message(), h, m.Protocol(), m.Sender())
	}
}

func (p *Protocol) getPriority(protoName string) priorityq.Priority {
	v, exist := p.priorities[protoName]
	if !exist {
		p.With().Warning("note: no priority found for protocol", log.String("protoName", protoName))
		return priorityq.Low
	}
	return v
}

// pushes messages that passed validation into the priority queue
func (p *Protocol) propagationEventLoop() {
	go p.handlePQ()

	for {
		select {
		case msgV := <-p.propagateQ:
			if err := p.pq.Write(p.getPriority(msgV.Protocol()), msgV); err != nil {
				p.With().Error("fatal: could not write to priority queue", log.Err(err), log.String("protocol", msgV.Protocol()))
			}
			metrics.PropagationQueueLen.Set(float64(len(p.propagateQ)))

		case <-p.shutdown:
			p.pq.Close()
			p.Error("propagate event loop stopped: protocol shutdown")
			return
		}
	}
}
