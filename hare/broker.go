package hare

import (
	"context"
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

const inboxCapacity = 1024 // inbox size per instance

type startInstanceError error

type syncStateFunc func(context.Context) bool

type validator interface {
	Validate(context.Context, *Msg) bool
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	util.Closer
	log.Log
	mu             sync.RWMutex
	network        NetworkService
	eValidator     validator     // provides eligibility validation
	stateQuerier   StateQuerier  // provides activeness check
	isNodeSynced   syncStateFunc // provider function to check if the node is currently synced
	layersPerEpoch uint16
	inbox          chan service.GossipMessage
	queue          priorityq.PriorityQueue
	queueChannel   chan struct{} // used to synchronize the message queues
	syncState      map[uint32]bool
	outbox         map[uint32]chan *Msg
	pending        map[uint32][]*Msg // the buffer of pending early messages for the next layer
	tasks          chan func()       // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayerMu  sync.RWMutex
	latestLayer    types.LayerID // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     types.LayerID
	limit          int // max number of simultaneous consensus processes
}

func newBroker(networkService NetworkService, eValidator validator, stateQuerier StateQuerier, syncState syncStateFunc, layersPerEpoch uint16, limit int, closer util.Closer, log log.Log) *Broker {
	return &Broker{
		Closer:         closer,
		Log:            log,
		network:        networkService,
		eValidator:     eValidator,
		stateQuerier:   stateQuerier,
		isNodeSynced:   syncState,
		layersPerEpoch: layersPerEpoch,
		syncState:      make(map[uint32]bool),
		outbox:         make(map[uint32]chan *Msg),
		pending:        make(map[uint32][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    types.GetEffectiveGenesis(),
		limit:          limit,
		queue:          priorityq.New(inboxCapacity),       // TODO: set capacity correctly
		queueChannel:   make(chan struct{}, inboxCapacity), // TODO: set capacity correctly
	}
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start(ctx context.Context) error {
	if b.isStarted { // Start has been called at least twice
		b.WithContext(ctx).Error("could not start instance")
		return startInstanceError(errors.New("instance already started"))
	}

	inbox := b.network.RegisterGossipProtocol(protoName, priorityq.Mid)
	return b.startWithInbox(ctx, inbox)
}

func (b *Broker) startWithInbox(ctx context.Context, inbox chan service.GossipMessage) error {
	b.isStarted = true
	b.inbox = inbox

	go b.queueLoop(log.WithNewSessionID(ctx))
	go b.eventLoop(log.WithNewSessionID(ctx))

	return nil
}

var (
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
	errInstanceNotSynced = errors.New("instance not synchronized")
)

// validate the message is contextually valid and that the target layer is synced.
// note: it is important to check synchronicity after contextual to avoid memory leak in syncState.
func (b *Broker) validate(ctx context.Context, m *Message) error {
	msgInstID := m.InnerMsg.InstanceID

	b.mu.RLock()
	_, exist := b.outbox[msgInstID.Uint32()]
	b.mu.RUnlock()

	if !exist {
		// prev layer, must be unregistered
		latestLayer := b.getLatestLayer()

		if msgInstID.Before(latestLayer) {
			return errUnregistered
		}

		// current layer
		if msgInstID == latestLayer {
			return errRegistration
		}

		// early msg
		if msgInstID == latestLayer.Add(1) {
			return errEarlyMsg
		}

		// future msg
		return errFutureMsg
	}

	// exist, check synchronicity
	if !b.isSynced(ctx, msgInstID) {
		return errNotSynced
	}

	// synced and has instance
	return nil
}

// separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) queueLoop(ctx context.Context) {
	for {
		select {
		case msg := <-b.inbox:
			logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", b.getLatestLayer()))
			logger.Debug("hare broker received inbound gossip message",
				log.Int("inbox_queue_len", len(b.inbox)))
			if msg == nil {
				logger.Error("broker message validation failed: called with nil")
				continue
			}

			// prioritize based on signature: outbound messages (self-generated) get priority
			priority := priorityq.Mid
			if msg.IsOwnMessage() {
				priority = priorityq.High
			}
			logger.With().Debug("assigned message priority, writing to priority queue",
				log.Int("priority", int(priority)))

			if err := b.queue.Write(priority, msg); err != nil {
				logger.With().Error("error writing inbound message to priority queue, dropping", log.Err(err))
			}

			// indicate to the listener that there's a new message in the queue
			b.queueChannel <- struct{}{}

		case <-b.CloseChannel():
			b.Info("broker exiting")
			b.queue.Close()

			// release the listener to notice that the queue is closing
			close(b.queueChannel)
			return
		}
	}
}

// listens to incoming messages and incoming tasks.
func (b *Broker) eventLoop(ctx context.Context) {
	for {
		b.WithContext(ctx).With().Debug("broker queue sizes",
			log.Int("msg_queue_size", len(b.queueChannel)),
			log.Int("task_queue_size", len(b.tasks)))
		select {
		case <-b.queueChannel:
			logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", b.getLatestLayer()))
			rawMsg, err := b.queue.Read()
			if err != nil {
				logger.With().Info("priority queue was closed, exiting", log.Err(err))
				return
			}
			msg, ok := rawMsg.(service.GossipMessage)
			if !ok {
				logger.Error("could not convert priority queue message, ignoring")
				continue
			}

			// create an inner context object to handle this message
			messageCtx := ctx

			// try to read stored context
			if msg.RequestID() == "" {
				logger.With().Warning("broker received hare message with no registered requestID")
			} else {
				messageCtx = log.WithRequestID(ctx, msg.RequestID())
			}

			h := types.CalcMessageHash12(msg.Bytes(), protoName)
			msgLogger := logger.WithContext(messageCtx).WithFields(h)
			hareMsg, err := MessageFromBuffer(msg.Bytes())
			if err != nil {
				msgLogger.With().Error("could not build message", h, log.Err(err))
				continue
			}
			msgLogger = msgLogger.WithFields(hareMsg)

			if hareMsg.InnerMsg == nil {
				msgLogger.With().Error("broker message validation failed", log.Err(errNilInner))
				continue
			}
			msgLogger.With().Debug("broker received hare message")

			msgInstID := hareMsg.InnerMsg.InstanceID
			metrics.MessageTypeCounter.With(
				"type_id", hareMsg.InnerMsg.Type.String(),
				"layer", msgInstID.String(),
				"reporter", "brokerHandler").Add(1)
			msgLogger = msgLogger.WithFields(log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)))
			isEarly := false
			if err := b.validate(messageCtx, hareMsg); err != nil {
				if !errors.Is(err, errEarlyMsg) {
					// not early, validation failed
					msgLogger.With().Debug("broker received a message to a consensus process that is not registered",
						log.Err(err))
					continue
				}

				msgLogger.With().Debug("early message detected", log.Err(err))
				isEarly = true
			}

			// create msg
			iMsg, err := newMsg(messageCtx, b.Log, hareMsg, b.stateQuerier)
			if err != nil {
				msgLogger.With().Warning("message validation failed: could not construct msg", log.Err(err))
				continue
			}

			// validate msg
			if !b.eValidator.Validate(messageCtx, iMsg) {
				msgLogger.With().Warning("message validation failed: eligibility validator returned false",
					log.String("hare_msg", hareMsg.String()))
				continue
			}

			// validation passed, report
			msg.ReportValidation(ctx, protoName)
			msgLogger.With().Debug("broker reported hare message as valid", hareMsg)

			if isEarly {
				b.mu.Lock()
				if _, exist := b.pending[msgInstID.Uint32()]; !exist { // create buffer if first msg
					b.pending[msgInstID.Uint32()] = make([]*Msg, 0)
				}
				b.mu.Unlock()

				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				b.mu.RLock()
				chCount := len(b.pending[msgInstID.Uint32()])
				b.mu.RUnlock()
				if chCount == inboxCapacity {
					msgLogger.With().Error("too many pending messages, ignoring message",
						log.Int("inbox_capacity", inboxCapacity),
						log.String("sender_id", iMsg.PubKey.ShortString()))
					continue
				}
				b.mu.Lock()
				b.pending[msgInstID.Uint32()] = append(b.pending[msgInstID.Uint32()], iMsg)
				b.mu.Unlock()
				continue
			}

			// has instance, just send
			b.mu.RLock()
			out, exist := b.outbox[msgInstID.Uint32()]
			b.mu.RUnlock()
			if !exist {
				msgLogger.With().Panic("missing broker instance for layer")
			}
			msgLogger.With().Debug("broker forwarding message to outbox",
				log.Int("outbox_queue_size", len(out)))
			out <- iMsg

		case task := <-b.tasks:
			latestLayer := b.getLatestLayer()

			b.WithContext(ctx).With().Debug("broker received task, executing",
				log.FieldNamed("latest_layer", latestLayer))
			task()
			b.WithContext(ctx).With().Debug("broker finished executing task",
				log.FieldNamed("latest_layer", latestLayer))
		}
	}
}

func (b *Broker) updateLatestLayer(ctx context.Context, id types.LayerID) {
	if !id.After(b.getLatestLayer()) { // should expect to update only newer layers
		b.WithContext(ctx).With().Panic("tried to update a previous layer",
			log.FieldNamed("this_layer", id),
			log.FieldNamed("prev_layer", b.getLatestLayer()))
		return
	}

	b.setLatestLayer(id)
}

func (b *Broker) cleanOldLayers() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := b.minDeleted.Add(1); i.Before(b.getLatestLayer()); i = i.Add(1) {
		_, exist := b.outbox[i.Uint32()]

		if !exist { // unregistered
			delete(b.syncState, i.Uint32()) // clean sync state
			b.minDeleted = b.minDeleted.Add(1)
		} else { // encountered first still running layer
			break
		}
	}
}

func (b *Broker) updateSynchronicity(ctx context.Context, id types.LayerID) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.syncState[id.Uint32()]; ok { // already has result
		return
	}

	// not exist means unknown, check & set

	if !b.isNodeSynced(ctx) {
		b.WithContext(ctx).With().Info("node is not synced, marking layer as not synced", types.LayerID(id))
		b.syncState[id.Uint32()] = false // mark not synced
		return
	}

	b.syncState[id.Uint32()] = true // mark valid
}

func (b *Broker) isSynced(ctx context.Context, id types.LayerID) bool {
	b.updateSynchronicity(ctx, id)

	b.mu.RLock()
	synced, ok := b.syncState[id.Uint32()]
	b.mu.RUnlock()
	if !ok { // not exist means unknown
		b.WithContext(ctx).Panic("syncState doesn't contain a value after call to updateSynchronicity")
	}

	return synced
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan *Msg, error) {
	resErr := make(chan error, 1)
	resCh := make(chan chan *Msg, 1)
	regRequest := func() {
		ctx := log.WithNewSessionID(ctx)
		b.updateLatestLayer(ctx, id)

		// first performing a check to see whether we are synced for this layer
		if b.isSynced(ctx, id) {
			// This section of code does not need to be protected against possible race conditions
			// because this function will be added to a queue of tasks that will all be
			// executed sequentially and synchronously. There is still the concern that two or more
			// calls to Register will be executed out of order, but Register is only called
			// on a new layer tick, and anyway updateLatestLayer would panic in this case.
			b.mu.RLock()
			outboxLen := len(b.outbox)
			b.mu.RUnlock()
			if outboxLen >= b.limit {
				// unregister the earliest layer to make space for the new layer
				// cannot call unregister here because unregister blocks and this would cause a deadlock
				b.mu.RLock()
				instance := b.minDeleted.Add(1)
				b.mu.RUnlock()
				b.cleanState(instance)
				b.With().Info("unregistered layer due to maximum concurrent processes", types.LayerID(instance))
			}

			outboxCh := make(chan *Msg, inboxCapacity)

			b.mu.Lock()
			b.outbox[id.Uint32()] = outboxCh
			pendingForInstance := b.pending[id.Uint32()]
			b.mu.Unlock()

			if pendingForInstance != nil {
				for _, mOut := range pendingForInstance {
					outboxCh <- mOut
				}
				b.mu.Lock()
				delete(b.pending, id.Uint32())
				b.mu.Unlock()
			}

			resErr <- nil
			resCh <- outboxCh

			return
		}

		// if we are not synced, we return an InstanceNotSynced error
		resErr <- errInstanceNotSynced
		resCh <- nil
	}

	b.WithContext(ctx).With().Debug("queueing register task", types.LayerID(id))
	b.tasks <- regRequest // send synced task

	// wait for result
	err := <-resErr
	result := <-resCh
	b.WithContext(ctx).With().Debug("register task result received", types.LayerID(id), log.Err(err))
	if err != nil { // reg failed
		return nil, err
	}

	return result, nil // reg ok
}

func (b *Broker) cleanState(id types.LayerID) {
	b.mu.Lock()
	delete(b.outbox, id.Uint32())
	b.mu.Unlock()

	b.cleanOldLayers()
}

// Unregister a layer from receiving messages.
func (b *Broker) Unregister(ctx context.Context, id types.LayerID) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		b.cleanState(id)
		b.WithContext(ctx).With().Info("hare broker unregistered layer", types.LayerID(id))
		wg.Done()
	}

	wg.Wait()
}

// Synced returns true if the given layer is synced, false otherwise.
func (b *Broker) Synced(ctx context.Context, id types.LayerID) bool {
	res := make(chan bool)
	b.tasks <- func() {
		res <- b.isSynced(ctx, id)
	}

	return <-res
}

func (b *Broker) getLatestLayer() types.LayerID {
	b.latestLayerMu.RLock()
	defer b.latestLayerMu.RUnlock()

	return b.latestLayer
}

func (b *Broker) setLatestLayer(layer types.LayerID) {
	b.latestLayerMu.Lock()
	defer b.latestLayerMu.Unlock()

	b.latestLayer = layer
}
