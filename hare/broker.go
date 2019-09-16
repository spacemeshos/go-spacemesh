package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"sync"
)

const InboxCapacity = 1024 // inbox size per instance

type StartInstanceError error

type syncStateFunc func() bool

type Validator interface {
	Validate(m *Msg) bool
}

// Closer is used to add closeability to an object
type Closer struct {
	channel chan struct{} // closeable go routines listen to this channel
}

func NewCloser() Closer {
	return Closer{make(chan struct{})}
}

// Closes all listening instances (should be called only once)
func (closer *Closer) Close() {
	close(closer.channel)
}

// CloseChannel returns the channel to wait on
func (closer *Closer) CloseChannel() chan struct{} {
	return closer.channel
}

// Broker is responsible for dispatching hare Messages to the matching set objectId listener
type Broker struct {
	Closer
	log.Log
	network        NetworkService
	eValidator     Validator
	stateQuerier   StateQuerier
	isNodeSynced   syncStateFunc
	layersPerEpoch uint16
	inbox          chan service.GossipMessage
	syncState      map[InstanceId]bool
	outbox         map[InstanceId]chan *Msg
	pending        map[InstanceId][]*Msg
	tasks          chan func()
	latestLayer    InstanceId
	isStarted      bool
	lastDeleted    InstanceId
}

func NewBroker(networkService NetworkService, eValidator Validator, stateQuerier StateQuerier, syncState syncStateFunc, layersPerEpoch uint16, closer Closer, log log.Log) *Broker {
	return &Broker{
		Closer:         closer,
		Log:            log,
		network:        networkService,
		eValidator:     eValidator,
		stateQuerier:   stateQuerier,
		isNodeSynced:   syncState,
		layersPerEpoch: layersPerEpoch,
		syncState:      make(map[InstanceId]bool),
		outbox:         make(map[InstanceId]chan *Msg),
		pending:        make(map[InstanceId][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    0,
		lastDeleted:    0,
	}
}

// Start listening to protocol Messages and dispatch Messages (non-blocking)
func (b *Broker) Start() error {
	if b.isStarted { // Start has been called at least twice
		b.Error("Could not start instance")
		return StartInstanceError(errors.New("instance already started"))
	}

	b.isStarted = true

	b.inbox = b.network.RegisterGossipProtocol(protoName)
	go b.eventLoop()

	return nil
}

var (
	errUnregistered = errors.New("layer is unregistered")
	errNotSynced    = errors.New("layer is not synced")
	errFutureMsg    = errors.New("future message")
	errRegistration = errors.New("failed during registration")
)

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

// Dispatch incoming Messages to the matching set objectId instance
func (b *Broker) eventLoop() {
	for {
		select {
		case msg := <-b.inbox:
			if msg == nil {
				b.With().Error("Message validation failed: called with nil",
					log.Uint64("latest_layer", uint64(b.latestLayer)))
				continue
			}

			hareMsg, err := MessageFromBuffer(msg.Bytes())
			if err != nil {
				b.Error("Could not build message err=%v", err)
				continue
			}

			if hareMsg.InnerMsg == nil {
				b.With().Error("Message validation failed",
					log.Err(errNilInner), log.Uint64("latest_layer", uint64(b.latestLayer)))
				continue
			}

			msgInstId := hareMsg.InnerMsg.InstanceId
			isEarly := false
			if err := b.validate(hareMsg); err != nil {
				if err != errEarlyMsg {
					// not early, validation failed
					b.With().Info("Message contextual validation failed",
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
				if len(b.pending[msgInstId]) == InboxCapacity {
					b.Error("Reached %v pending messages. Ignoring message for layer %v sent from %v",
						InboxCapacity, msgInstId, iMsg.PubKey.ShortString())
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

func (b *Broker) updateLatestLayer(id InstanceId) {
	if id <= b.latestLayer { // should expect to update only newer layers
		b.Panic("Tried to update a previous layer expected %v > %v", id, b.latestLayer)
		return
	}

	b.latestLayer = id
}

func (b *Broker) cleanOldLayers() {
	for i := b.lastDeleted; i < b.latestLayer; i++ {
		if !b.syncState[i] { // invalid
			delete(b.syncState, i) // remove it
		}
	}
}

func (b *Broker) updateSynchronicity(id InstanceId) {
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

func (b *Broker) isSynced(id InstanceId) bool {
	b.updateSynchronicity(id)

	synced, ok := b.syncState[id]
	if !ok { // not exist means unknown
		log.Panic("syncState doesn't contain a value after call to updateSynchronicity")
	}

	return synced
}

// Register a listener to Messages
// Note: the registering instance is assumed to be started and accepting Messages
func (b *Broker) Register(id InstanceId) (chan *Msg, error) {
	res := make(chan chan *Msg, 1)
	regRequest := func() {
		b.updateLatestLayer(id)

		if b.isSynced(id) {
			b.outbox[id] = make(chan *Msg, InboxCapacity)

			pendingForInstance := b.pending[id]
			if pendingForInstance != nil {
				for _, mOut := range pendingForInstance {
					b.outbox[id] <- mOut
				}
				delete(b.pending, id)
			}

			res <- b.outbox[id]
			return
		}

		res <- nil
	}

	b.tasks <- regRequest // send synced task
	result := <-res       // wait for result
	if result == nil {    // reg failed
		return nil, errors.New("instance not synchronized")
	}

	return result, nil // reg ok
}

// Unregister a listener
func (b *Broker) Unregister(id InstanceId) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		delete(b.outbox, id)    // delete matching outbox
		delete(b.syncState, id) // delete matching state
		b.Info("Unregistered layer %v ", id)
		wg.Done()
	}

	wg.Wait()
}
