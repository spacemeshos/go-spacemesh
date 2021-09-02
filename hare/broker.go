package hare

import (
	"context"
	"errors"
	"strconv"
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
	network        NetworkService
	eValidator     validator     // provides eligibility validation
	stateQuerier   StateQuerier  // provides activeness check
	isNodeSynced   syncStateFunc // provider function to check if the node is currently synced
	layersPerEpoch uint16
	inbox          chan service.GossipMessage
	queue          priorityq.PriorityQueue
	queueChannel   chan struct{} // used to synchronize the message queues
	syncState      map[instanceID]bool
	outbox         map[instanceID]chan *Msg
	pending        map[instanceID][]*Msg // the buffer of pending early messages for the next layer
	tasks          chan func()           // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayer    instanceID            // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     instanceID
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
		syncState:      make(map[instanceID]bool),
		outbox:         make(map[instanceID]chan *Msg),
		pending:        make(map[instanceID][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    instanceID(types.GetEffectiveGenesis()),
		minDeleted:     0,
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
	if !b.isSynced(ctx, msgInstID) {
		return errNotSynced
	}

	// synced and has instance
	return nil
}

// separate listener routine that receives gossip messages and adds them to the priority queue
func (b *Broker) queueLoop(ctx context.Context) {
	for {
		select {
		case msg := <-b.inbox:
			logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
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

// listens to incoming messages and incoming tasks
func (b *Broker) eventLoop(ctx context.Context) {
	for {
		b.WithContext(ctx).With().Debug("broker queue sizes",
			log.Int("msg_queue_size", len(b.queueChannel)),
			log.Int("task_queue_size", len(b.tasks)))
		select {
		case <-b.queueChannel:
			logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
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
			metrics.MessageTypeCounter.With("type_id", hareMsg.InnerMsg.Type.String(), "layer", strconv.FormatUint(uint64(msgInstID), 10), "reporter", "brokerHandler").Add(1)
			msgLogger = msgLogger.WithFields(log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)))
			isEarly := false
			if err := b.validate(messageCtx, hareMsg); err != nil {
				if err != errEarlyMsg {
					// not early, validation failed
					msgLogger.With().Debug("broker received a message to a consensus process that is not registered",
						log.Err(err))
					continue
				}

				msgLogger.With().Debug("early message detected", log.Err(err))
				isEarly = true
			}

			// create msg
			iMsg, err := newMsg(messageCtx, hareMsg, b.stateQuerier)
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
			msgLogger.With().Info("broker reported hare message as valid", hareMsg)

			if isEarly {
				if _, exist := b.pending[msgInstID]; !exist { // create buffer if first msg
					b.pending[msgInstID] = make([]*Msg, 0)
				}
				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				if len(b.pending[msgInstID]) == inboxCapacity {
					msgLogger.With().Error("too many pending messages, ignoring message",
						log.Int("inbox_capacity", inboxCapacity),
						log.String("sender_id", iMsg.PubKey.ShortString()))
					continue
				}
				b.pending[msgInstID] = append(b.pending[msgInstID], iMsg)
				continue
			}

			// has instance, just send
			out, exist := b.outbox[msgInstID]
			if !exist {
				msgLogger.With().Panic("missing broker instance for layer")
			}
			msgLogger.With().Debug("broker forwarding message to outbox",
				log.Int("outbox_queue_size", len(out)))
			out <- iMsg

		case task := <-b.tasks:
			b.WithContext(ctx).With().Debug("broker received task, executing",
				log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
			task()
			b.WithContext(ctx).With().Debug("broker finished executing task",
				log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
		}
	}
}

func (b *Broker) updateLatestLayer(ctx context.Context, id instanceID) {
	if id <= b.latestLayer { // should expect to update only newer layers
		b.WithContext(ctx).With().Panic("tried to update a previous layer",
			log.FieldNamed("this_layer", types.LayerID(id)),
			log.FieldNamed("prev_layer", types.LayerID(b.latestLayer)))
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

func (b *Broker) updateSynchronicity(ctx context.Context, id instanceID) {
	if _, ok := b.syncState[id]; ok { // already has result
		return
	}

	// not exist means unknown, check & set

	if !b.isNodeSynced(ctx) {
		b.WithContext(ctx).With().Info("node is not synced, marking layer as not synced", types.LayerID(id))
		b.syncState[id] = false // mark not synced
		return
	}

	b.syncState[id] = true // mark valid
}

func (b *Broker) isSynced(ctx context.Context, id instanceID) bool {
	b.updateSynchronicity(ctx, id)

	synced, ok := b.syncState[id]
	if !ok { // not exist means unknown
		b.WithContext(ctx).Panic("syncState doesn't contain a value after call to updateSynchronicity")
	}

	return synced
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages
func (b *Broker) Register(ctx context.Context, id instanceID) (chan *Msg, error) {
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

func (b *Broker) cleanState(id instanceID) {
	delete(b.outbox, id)
	b.cleanOldLayers()
}

// Unregister a layer from receiving messages
func (b *Broker) Unregister(ctx context.Context, id instanceID) {
	wg := sync.WaitGroup{}

	wg.Add(1)
	b.tasks <- func() {
		b.cleanState(id)
		b.WithContext(ctx).With().Info("hare broker unregistered layer", types.LayerID(id))
		wg.Done()
	}

	wg.Wait()
}

// Synced returns true if the given layer is synced, false otherwise
func (b *Broker) Synced(ctx context.Context, id instanceID) bool {
	res := make(chan bool)
	b.tasks <- func() {
		res <- b.isSynced(ctx, id)
	}

	return <-res
}
