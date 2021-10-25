package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/priorityq"
)

const inboxCapacity = 1024 // inbox size per instance

type startInstanceError error

type syncStateFunc func(context.Context) bool

type validator interface {
	Validate(context.Context, *Msg) bool
}

type msgRPC struct {
	Ctx   context.Context
	Data  []byte
	Error chan error
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	util.Closer
	log.Log
	pid            peer.ID
	eValidator     validator     // provides eligibility validation
	stateQuerier   StateQuerier  // provides activeness check
	isNodeSynced   syncStateFunc // provider function to check if the node is currently synced
	layersPerEpoch uint16
	queue          priorityq.PriorityQueue
	queueChannel   chan struct{} // used to synchronize the message queues
	syncState      map[uint32]bool
	outbox         map[uint32]chan *Msg
	pending        map[uint32][]*Msg // the buffer of pending early messages for the next layer
	tasks          chan func()       // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayer    types.LayerID     // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     types.LayerID
	limit          int // max number of simultaneous consensus processes
	stop           context.CancelFunc
}

func newBroker(pid peer.ID, eValidator validator, stateQuerier StateQuerier, syncState syncStateFunc, layersPerEpoch uint16, limit int, closer util.Closer, log log.Log) *Broker {
	return &Broker{
		Closer:         closer,
		Log:            log,
		pid:            pid,
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
		queue:          priorityq.New(inboxCapacity), // TODO: set capacity correctly
		queueChannel:   make(chan struct{}, inboxCapacity),
	}
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start(ctx context.Context) error {
	if b.isStarted { // Start has been called at least twice
		b.WithContext(ctx).Error("could not start instance")
		return startInstanceError(errors.New("instance already started"))
	}
	b.isStarted = true
	ctx, cancel := context.WithCancel(ctx)
	b.stop = cancel
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

	_, exist := b.outbox[msgInstID.Uint32()]

	if !exist {
		// prev layer, must be unregistered
		if msgInstID.Before(b.latestLayer) {
			return errUnregistered
		}

		// current layer
		if msgInstID == b.latestLayer {
			return errRegistration
		}

		// early msg
		if msgInstID == b.latestLayer.Add(1) {
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

// HandleMessage separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) HandleMessage(ctx context.Context, pid peer.ID, msg []byte) pubsub.ValidationResult {
	m, err := b.queueMessage(ctx, pid, msg)
	if err != nil {
		return pubsub.ValidationIgnore
	}
	select {
	case <-ctx.Done():
		return pubsub.ValidationIgnore
	case err := <-m.Error:
		if err != nil {
			b.Log.With().Warning("hare validation failed", log.Err(err))
			return pubsub.ValidationIgnore
		}
	}
	return pubsub.ValidationAccept
}

func (b *Broker) queueMessage(ctx context.Context, pid p2p.Peer, msg []byte) (*msgRPC, error) {
	logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", b.latestLayer))
	logger.Debug("hare broker received inbound gossip message")

	// prioritize based on signature: outbound messages (self-generated) get priority
	priority := priorityq.Mid
	if pid == b.pid {
		priority = priorityq.High
	}
	logger.With().Debug("assigned message priority, writing to priority queue",
		log.Int("priority", int(priority)))
	m := &msgRPC{Data: msg, Error: make(chan error, 1), Ctx: ctx}
	if err := b.queue.Write(priority, m); err != nil {
		logger.With().Error("error writing inbound message to priority queue, dropping", log.Err(err))
		return nil, fmt.Errorf("write inbound message to priority queue: %w", err)
	}

	// indicate to the listener that there's a new message in the queue
	select {
	case b.queueChannel <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return m, nil
}

// listens to incoming messages and incoming tasks.
func (b *Broker) eventLoop(ctx context.Context) {
	for {
		b.WithContext(ctx).With().Debug("broker queue sizes",
			log.Int("msg_queue_size", len(b.queueChannel)),
			log.Int("task_queue_size", len(b.tasks)))
		select {
		case <-b.Closer.CloseChannel():
			b.queue.Close()
			close(b.queueChannel)
			return
		case <-b.queueChannel:
			logger := b.WithContext(ctx).WithFields(log.FieldNamed("latest_layer", types.LayerID(b.latestLayer)))
			rawMsg, err := b.queue.Read()
			if err != nil {
				logger.With().Info("priority queue was closed, exiting", log.Err(err))
				return
			}
			msg, ok := rawMsg.(*msgRPC)
			if !ok {
				logger.Panic("could not convert priority queue message, ignoring")
			}

			// create an inner context object to handle this message
			messageCtx := msg.Ctx

			h := types.CalcMessageHash12(msg.Data, protoName)
			msgLogger := logger.WithContext(messageCtx).WithFields(h)
			hareMsg, err := MessageFromBuffer(msg.Data)
			if err != nil {
				msgLogger.With().Error("could not build message", h, log.Err(err))
				msg.Error <- err
				continue
			}
			msgLogger = msgLogger.WithFields(hareMsg)

			if hareMsg.InnerMsg == nil {
				msgLogger.With().Error("broker message validation failed", log.Err(errNilInner))
				msg.Error <- errNilInner
				continue
			}
			msgLogger.With().Debug("broker received hare message")

			msgInstID := hareMsg.InnerMsg.InstanceID
			metrics.MessageTypeCounter.With(prometheus.Labels{
				"type_id":  hareMsg.InnerMsg.Type.String(),
				"layer":    msgInstID.String(),
				"reporter": "brokerHandler",
			}).Inc()
			msgLogger = msgLogger.WithFields(log.FieldNamed("msg_layer_id", types.LayerID(msgInstID)))
			isEarly := false
			if err := b.validate(messageCtx, hareMsg); err != nil {
				if !errors.Is(err, errEarlyMsg) {
					// not early, validation failed
					msgLogger.With().Debug("broker received a message to a consensus process that is not registered",
						log.Err(err))
					msg.Error <- err
					continue
				}

				msgLogger.With().Debug("early message detected", log.Err(err))
				isEarly = true
			}

			// create msg
			iMsg, err := newMsg(messageCtx, b.Log, hareMsg, b.stateQuerier)
			if err != nil {
				msgLogger.With().Warning("message validation failed: could not construct msg", log.Err(err))
				msg.Error <- err
				continue
			}

			// validate msg
			if !b.eValidator.Validate(messageCtx, iMsg) {
				msgLogger.With().Warning("message validation failed: eligibility validator returned false",
					log.String("hare_msg", hareMsg.String()))
				msg.Error <- errors.New("not eligible")
				continue
			}

			// validation passed, report
			msg.Error <- nil
			msgLogger.With().Debug("broker reported hare message as valid", hareMsg)

			if isEarly {
				if _, exist := b.pending[msgInstID.Uint32()]; !exist { // create buffer if first msg
					b.pending[msgInstID.Uint32()] = make([]*Msg, 0)
				}
				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				if len(b.pending[msgInstID.Uint32()]) == inboxCapacity {
					msgLogger.With().Error("too many pending messages, ignoring message",
						log.Int("inbox_capacity", inboxCapacity),
						log.String("sender_id", iMsg.PubKey.ShortString()))
					continue
				}
				b.pending[msgInstID.Uint32()] = append(b.pending[msgInstID.Uint32()], iMsg)
				continue
			}

			// has instance, just send
			out, exist := b.outbox[msgInstID.Uint32()]
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

func (b *Broker) updateLatestLayer(ctx context.Context, id types.LayerID) {
	if !id.After(b.latestLayer) { // should expect to update only newer layers
		b.WithContext(ctx).With().Panic("tried to update a previous layer",
			log.FieldNamed("this_layer", id),
			log.FieldNamed("prev_layer", b.latestLayer))
		return
	}

	b.latestLayer = id
}

func (b *Broker) cleanOldLayers() {
	for i := b.minDeleted.Add(1); i.Before(b.latestLayer); i = i.Add(1) {
		if _, exist := b.outbox[i.Uint32()]; !exist { // unregistered
			delete(b.syncState, i.Uint32()) // clean sync state
			b.minDeleted = b.minDeleted.Add(1)
		} else { // encountered first still running layer
			break
		}
	}
}

func (b *Broker) updateSynchronicity(ctx context.Context, id types.LayerID) {
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

	synced, ok := b.syncState[id.Uint32()]
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
			if len(b.outbox) >= b.limit {
				// unregister the earliest layer to make space for the new layer
				// cannot call unregister here because unregister blocks and this would cause a deadlock
				instance := b.minDeleted.Add(1)
				b.cleanState(instance)
				b.With().Info("unregistered layer due to maximum concurrent processes", types.LayerID(instance))
			}

			b.outbox[id.Uint32()] = make(chan *Msg, inboxCapacity)

			pendingForInstance := b.pending[id.Uint32()]
			if pendingForInstance != nil {
				for _, mOut := range pendingForInstance {
					b.outbox[id.Uint32()] <- mOut
				}
				delete(b.pending, id.Uint32())
			}

			resErr <- nil
			resCh <- b.outbox[id.Uint32()]
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
	delete(b.outbox, id.Uint32())
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
