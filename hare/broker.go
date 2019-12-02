package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

const inboxCapacity = 1024 // inbox size per instance

type startInstanceError error

type syncStateFunc func() bool

type validator interface {
	Validate(m *Msg) bool
}

// Closer adds the ability to close objects.
type Closer struct {
	channel chan struct{} // closeable go routines listen to this channel
}

// NewCloser creates a new (not closed) closer.
func NewCloser() Closer {
	return Closer{make(chan struct{})}
}

// Close signals all listening instances to close.
// Note: should be called only once.
func (closer *Closer) Close() {
	close(closer.channel)
}

// CloseChannel returns the channel to wait on for close signal.
func (closer *Closer) CloseChannel() chan struct{} {
	return closer.channel
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	Closer
	log.Log
	network        NetworkService
	eValidator     validator     // provides eligibility validation
	stateQuerier   StateQuerier  // provides activeness check
	isNodeSynced   syncStateFunc // provider function to check if the node is currently synced
	layersPerEpoch uint16
	inbox          chan service.GossipMessage
	syncState      map[instanceId]bool
	outbox         map[instanceId]chan *Msg
	pending        map[instanceId][]*Msg // the buffer of pending messages for the next layer
	tasks          chan func()           // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayer    instanceId            // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     instanceId
	limit          int // max number of consensus processes simultaneously
}

func newBroker(networkService NetworkService, eValidator validator, stateQuerier StateQuerier, syncState syncStateFunc, layersPerEpoch uint16, limit int, closer Closer, log log.Log) *Broker {
	return &Broker{
		Closer:         closer,
		Log:            log,
		network:        networkService,
		eValidator:     eValidator,
		stateQuerier:   stateQuerier,
		isNodeSynced:   syncState,
		layersPerEpoch: layersPerEpoch,
		syncState:      make(map[instanceId]bool),
		outbox:         make(map[instanceId]chan *Msg),
		pending:        make(map[instanceId][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    0,
		minDeleted:     0,
		limit:          limit,
	}
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start() error {
	if b.isStarted { // Start has been called at least twice
		b.Error("Could not start instance")
		return startInstanceError(errors.New("instance already started"))
	}

	b.isStarted = true

	b.inbox = b.network.RegisterGossipProtocol(protoName)
	go b.eventLoop()

	return nil
}

var (
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
	errTooMany           = errors.New("too many consensus process")
	errInstanceNotSynced = errors.New("instance not synchronized")
)

// validate the message is contextually valid and that the target layer is synced.
// note: it is important to check synchronicity after contextual to avoid memory leak in syncState.
func (b *Broker) validate(m *Message) error {
	msgInstId := m.InnerMsg.InstanceId

	_, exist := b.outbox[msgInstId]

	if !exist {
		// prev layer, must be unregistered
		if msgInstId < b.latestLayer {
			return errUnregistered
		}

		// current layer
		if msgInstId == b.latestLayer {
			return errRegistration
		}

		// early msg
		if msgInstId == b.latestLayer+1 {
			return errEarlyMsg
		}

		// future msg
		return errFutureMsg
	}

	// exist, check synchronicity
	if !b.isSynced(msgInstId) {
		return errNotSynced
	}

	// synced and has instance
	return nil
}

// listens to incoming messages and incoming tasks
func (b *Broker) eventLoop() {
	for {
		select {
		case msg := <-b.inbox:
			if msg == nil {
				b.With().Error("Broker message validation failed: called with nil",
					log.Uint64("latest_layer", uint64(b.latestLayer)))
				continue
			}

			hareMsg, err := MessageFromBuffer(msg.Bytes())
			if err != nil {
				b.Error("Could not build message err=%v", err)
				continue
			}

			if hareMsg.InnerMsg == nil {
				b.With().Error("Broker message validation failed",
					log.Err(errNilInner), log.Uint64("latest_layer", uint64(b.latestLayer)))
				continue
			}

			msgInstId := hareMsg.InnerMsg.InstanceId
			isEarly := false
			if err := b.validate(hareMsg); err != nil {
				if err != errEarlyMsg {
					// not early, validation failed
					b.With().Debug("Broker received a message to a CP that is not registered",
						log.Err(err),
						log.Uint64("msg_layer_id", uint64(msgInstId)),
						log.Uint64("latest_layer", uint64(b.latestLayer)))
					continue
				}

				b.With().Debug("early message detected",
					log.Err(err),
					log.Uint64("msg_layer_id", uint64(msgInstId)),
					log.Uint64("latest_layer", uint64(b.latestLayer)))

				isEarly = true
			}

			// the msg is either early or has instance

			// create msg
			iMsg, err := newMsg(hareMsg, b.stateQuerier, b.layersPerEpoch)
			if err != nil {
				b.Warning("Message validation failed: could not construct msg err=%v", err)
				continue
			}

			// validate msg
			if !b.eValidator.Validate(iMsg) {
				b.Warning("Message validation failed: eValidator returned false %v", hareMsg)
				continue
			}

			// validation passed, report
			msg.ReportValidation(protoName)

			if isEarly {
				if _, exist := b.pending[msgInstId]; !exist { // create buffer if first msg
					b.pending[msgInstId] = make([]*Msg, 0)
				}
				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				if len(b.pending[msgInstId]) == inboxCapacity {
					b.Error("Reached %v pending messages. Ignoring message for layer %v sent from %v",
						inboxCapacity, msgInstId, iMsg.PubKey.ShortString())
					continue
				}
				b.pending[msgInstId] = append(b.pending[msgInstId], iMsg)
				continue
			}

			// has instance, just send
			out, exist := b.outbox[msgInstId]
			if !exist {
				b.Panic("broker should have had an instance for layer %v", msgInstId)
			}
			out <- iMsg

		case task := <-b.tasks:
			task()
		case <-b.CloseChannel():
			b.Warning("Broker exiting")
			return
		}
	}
}

func (b *Broker) updateLatestLayer(id instanceId) {
	if id <= b.latestLayer { // should expect to update only newer layers
		b.Panic("Tried to update a previous layer expected %v > %v", id, b.latestLayer)
		return
	}

	b.latestLayer = id
}

func (b *Broker) cleanOldLayers() {
	for i := b.minDeleted + 1; i < b.latestLayer; i++ {
		if _, exist := b.outbox[i]; !exist { // unregistered
			delete(b.syncState, i) // clean sync state
			b.minDeleted++
		} else { // encountered first still running layer
			break
		}
	}
}

func (b *Broker) updateSynchronicity(id instanceId) {
	if _, ok := b.syncState[id]; ok { // already has result
		return
	}

	// not exist means unknown, check & set

	if !b.isNodeSynced() {
		b.With().Info("Note: node is not synced. Marking layer as not synced", log.LayerId(uint64(id)))
		b.syncState[id] = false // mark not synced
		return
	}

	b.syncState[id] = true // mark valid
}

func (b *Broker) isSynced(id instanceId) bool {
	b.updateSynchronicity(id)

	synced, ok := b.syncState[id]
	if !ok { // not exist means unknown
		log.Panic("syncState doesn't contain a value after call to updateSynchronicity")
	}

	return synced
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages
func (b *Broker) Register(id instanceId) (chan *Msg, error) {
	resErr := make(chan error, 1)
	resCh := make(chan chan *Msg, 1)
	regRequest := func() {
		b.updateLatestLayer(id)

		if len(b.outbox) >= b.limit {
			resErr <- errTooMany
			resCh <- nil
			return
		}

		if b.isSynced(id) {
			b.outbox[id] = make(chan *Msg, inboxCapacity)

			pendingForInstance := b.pending[id]
			if pendingForInstance != nil {
				for _, mOut := range pendingForInstance {
					b.outbox[id] <- mOut
				}
				delete(b.pending, id)
			}

			resErr <- nil
			resCh <- b.outbox[id]
			return
		}

		resErr <- errInstanceNotSynced
		resCh <- nil
	}

	b.tasks <- regRequest // send synced task

	// wait for result
	err := <-resErr
	result := <-resCh
	if err != nil { // reg failed
		return nil, err
	}

	return result, nil // reg ok
}

// Unregister a layer from receiving messages
func (b *Broker) Unregister(id instanceId) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		delete(b.outbox, id) // delete matching outbox
		b.cleanOldLayers()
		b.Info("Unregistered layer %v ", id)
		wg.Done()
	}

	wg.Wait()
}
