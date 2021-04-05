package hare

import (
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
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
	syncState      map[instanceID]bool
	outbox         map[instanceID]chan *Msg
	pending        map[instanceID][]*Msg // the buffer of pending messages for the next layer
	tasks          chan func()           // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayer    instanceID            // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     instanceID
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
		syncState:      make(map[instanceID]bool),
		outbox:         make(map[instanceID]chan *Msg),
		pending:        make(map[instanceID][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    instanceID(types.GetEffectiveGenesis()),
		minDeleted:     0,
		limit:          limit,
	}
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start() error {
	if b.isStarted { // Start has been called at least twice
		b.Error("could not start instance")
		return startInstanceError(errors.New("instance already started"))
	}

	b.isStarted = true

	b.inbox = b.network.RegisterGossipProtocol(protoName, priorityq.Mid)
	go b.eventLoop()

	return nil
}

var (
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
	errTooMany           = errors.New("too many concurrent consensus processes running")
	errInstanceNotSynced = errors.New("instance not synchronized")
)

// validate the message is contextually valid and that the target layer is synced.
// note: it is important to check synchronicity after contextual to avoid memory leak in syncState.
func (b *Broker) validate(m *Message) error {
	msgInstID := m.InnerMsg.InstanceID

	_, exist := b.outbox[msgInstID]

	if !exist {
		// prev layer, must be unregistered
		if msgInstID < b.latestLayer {
			return errUnregistered
		}

		// current layer
		if msgInstID == b.latestLayer {
			return errRegistration
		}

		// early msg
		if msgInstID == b.latestLayer+1 {
			return errEarlyMsg
		}

		// future msg
		return errFutureMsg
	}

	// exist, check synchronicity
	if !b.isSynced(msgInstID) {
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
				b.With().Error("broker message validation failed: called with nil",
					log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
				continue
			}

			h := types.CalcMessageHash12(msg.Bytes(), protoName)
			hareMsg, err := MessageFromBuffer(msg.Bytes())
			if err != nil {
				b.With().Error("could not build message", h, log.Err(err))
				continue
			}

			if hareMsg.InnerMsg == nil {
				b.With().Error("broker message validation failed",
					h,
					log.Err(errNilInner),
					log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
				continue
			}
			b.With().Debug("broker received hare message", hareMsg)

			// TODO: fix metrics
			// metrics.MessageTypeCounter.With("type_id", hareMsg.InnerMsg.Type.String(), "layer", strconv.FormatUint(uint64(msgInstID), 10), "reporter", "brokerHandler").Add(1)
			msgInstID := hareMsg.InnerMsg.InstanceID
			isEarly := false
			if err := b.validate(hareMsg); err != nil {
				if err != errEarlyMsg {
					// not early, validation failed
					b.With().Debug("broker received a message to a consensus process that is not registered",
						h,
						log.Err(err),
						hareMsg,
						log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)),
						log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
					continue
				}

				b.With().Debug("early message detected",
					h,
					log.Err(err),
					hareMsg,
					log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)),
					log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))

				isEarly = true
			}

			// the msg is either early or has instance

			// create msg
			iMsg, err := newMsg(hareMsg, b.stateQuerier)
			if err != nil {
				b.With().Warning("message validation failed: could not construct msg",
					h,
					hareMsg,
					log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)),
					log.Err(err))
				continue
			}

			// validate msg
			if !b.eValidator.Validate(iMsg) {
				b.With().Warning("message validation failed: eligibility validator returned false",
					h,
					hareMsg,
					log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)),
					log.String("hare_msg", hareMsg.String()))
				continue
			}

			// validation passed, report
			msg.ReportValidation(protoName)

			if isEarly {
				if _, exist := b.pending[msgInstID]; !exist { // create buffer if first msg
					b.pending[msgInstID] = make([]*Msg, 0)
				}
				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				if len(b.pending[msgInstID]) == inboxCapacity {
					b.With().Error("too many pending messages, ignoring message",
						log.Int("inbox_capacity", inboxCapacity),
						types.LayerID(msgInstID),
						log.String("sender_id", iMsg.PubKey.ShortString()))
					continue
				}
				b.pending[msgInstID] = append(b.pending[msgInstID], iMsg)
				continue
			}

			// has instance, just send
			out, exist := b.outbox[msgInstID]
			if !exist {
				b.Panic("broker should have had an instance for layer %v", msgInstID)
			}
			out <- iMsg

		case task := <-b.tasks:
			task()
		case <-b.CloseChannel():
			b.Warning("broker exiting")
			return
		}
	}
}

func (b *Broker) updateLatestLayer(id instanceID) {
	if id <= b.latestLayer { // should expect to update only newer layers
		b.Panic("tried to update a previous layer: expected %v > %v", id, b.latestLayer)
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

func (b *Broker) updateSynchronicity(id instanceID) {
	if _, ok := b.syncState[id]; ok { // already has result
		return
	}

	// not exist means unknown, check & set

	if !b.isNodeSynced() {
		b.With().Info("node is not synced, marking layer as not synced", types.LayerID(id))
		b.syncState[id] = false // mark not synced
		return
	}

	b.syncState[id] = true // mark valid
}

func (b *Broker) isSynced(id instanceID) bool {
	b.updateSynchronicity(id)

	synced, ok := b.syncState[id]
	if !ok { // not exist means unknown
		b.Panic("syncState doesn't contain a value after call to updateSynchronicity")
	}

	return synced
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages
func (b *Broker) Register(id instanceID) (chan *Msg, error) {
	resErr := make(chan error, 1)
	resCh := make(chan chan *Msg, 1)
	regRequest := func() {
		b.updateLatestLayer(id)

		//first performing a check to see whether we are synced for this layer
		if b.isSynced(id) {
			if len(b.outbox) >= b.limit {
				//unregister the earliest layer to make space for the new layer
				//cannot call unregister here because unregister blocks and this would cause a deadlock
				instance := b.minDeleted + 1
				b.cleanState(instance)
				b.With().Info("unregistered layer due to maximum concurrent processes", types.LayerID(instance))
			}

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
		} else {
			resErr <- errInstanceNotSynced
			resCh <- nil
			return
		}
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

func (b *Broker) cleanState(id instanceID) {
	delete(b.outbox, id)
	b.cleanOldLayers()
}

// Unregister a layer from receiving messages
func (b *Broker) Unregister(id instanceID) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		b.cleanState(id)
		b.With().Info("hare broker unregistered layer", types.LayerID(id))
		wg.Done()
	}

	wg.Wait()
}

// Synced returns true if the given layer is synced, false otherwise
func (b *Broker) Synced(id instanceID) bool {
	res := make(chan bool)
	b.tasks <- func() {
		res <- b.isSynced(id)
	}

	return <-res
}
