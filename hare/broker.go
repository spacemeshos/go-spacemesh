package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/system"
)

const inboxCapacity = 1024 // inbox size per instance

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
	log.Log
	mu             sync.RWMutex
	peer           p2p.Peer
	eValidator     validator                // provides eligibility validation
	stateQuerier   stateQuerier             // provides activeness check
	nodeSyncState  system.SyncStateProvider // provider function to check if the node is currently synced
	layersPerEpoch uint16
	queue          priorityq.PriorityQueue
	queueChannel   chan struct{} // used to synchronize the message queues
	outbox         map[uint32]chan *Msg
	pending        map[uint32][]*Msg // the buffer of pending early messages for the next layer
	tasks          chan func()       // a channel to synchronize tasks (register/unregister) with incoming messages handling
	latestLayerMu  sync.RWMutex
	latestLayer    types.LayerID // the latest layer to attempt register (successfully or unsuccessfully)
	isStarted      bool
	minDeleted     types.LayerID
	limit          int // max number of simultaneous consensus processes
	ctx            context.Context
	cancel         context.CancelFunc
	eg             errgroup.Group
}

func newBroker(peer p2p.Peer, eValidator validator, stateQuerier stateQuerier, syncState system.SyncStateProvider, layersPerEpoch uint16, limit int, log log.Log) *Broker {
	b := &Broker{
		Log:            log,
		peer:           peer,
		eValidator:     eValidator,
		stateQuerier:   stateQuerier,
		nodeSyncState:  syncState,
		layersPerEpoch: layersPerEpoch,
		outbox:         make(map[uint32]chan *Msg),
		pending:        make(map[uint32][]*Msg),
		tasks:          make(chan func()),
		latestLayer:    types.GetEffectiveGenesis(),
		limit:          limit,
		minDeleted:     types.GetEffectiveGenesis(),
		queue:          priorityq.New(context.Background()),
		queueChannel:   make(chan struct{}, inboxCapacity),
	}
	b.ctx, b.cancel = context.WithCancel(context.Background())
	return b
}

// Start listening to Hare messages (non-blocking).
func (b *Broker) Start(ctx context.Context) error {
	if b.isStarted { // Start has been called at least twice
		b.WithContext(ctx).Error("could not start instance")
		return errors.New("hare broker already started")
	}
	b.isStarted = true
	{
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.cancel != nil {
			b.cancel()
		}
		b.ctx, b.cancel = context.WithCancel(ctx)
		b.queue = priorityq.New(b.ctx)
	}

	b.eg.Go(func() error {
		b.eventLoop(ctx)
		return nil
	})

	return nil
}

var (
	errUnregistered      = errors.New("layer is unregistered")
	errNotSynced         = errors.New("layer is not synced")
	errBadMsg            = errors.New("bad message")
	errFutureMsg         = errors.New("future message")
	errRegistration      = errors.New("failed during registration")
	errInstanceNotSynced = errors.New("instance not synchronized")
	errClosed            = errors.New("closed")
)

// validate the message is contextually valid and that the node is synced for processing the message.
func (b *Broker) validate(ctx context.Context, m *Message) error {
	msgInstID := m.InnerMsg.Layer

	b.mu.RLock()
	_, exist := b.outbox[msgInstID.Uint32()]
	b.mu.RUnlock()

	if exist {
		b.Log.WithContext(ctx).With().Debug("instance exists", msgInstID)
		return nil
	}

	// prev layer, must be unregistered
	latestLayer := b.getLatestLayer()

	if msgInstID.Before(latestLayer) {
		return fmt.Errorf("%w: msg %v, latest %v", errUnregistered, msgInstID.Uint32(), latestLayer)
	}

	// current layer
	if msgInstID == latestLayer {
		return fmt.Errorf("%w: latest %v", errRegistration, latestLayer)
	}

	// early msg
	if msgInstID == latestLayer.Add(1) {
		return fmt.Errorf("%w: latest %v", errEarlyMsg, latestLayer)
	}

	// future msg
	return fmt.Errorf("%w: msg %v, latest %v", errFutureMsg, msgInstID, latestLayer)
}

// HandleMessage separate listener routine that receives gossip messages and adds them to the priority queue.
func (b *Broker) HandleMessage(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	m, err := b.queueMessage(ctx, peer, msg)
	if err != nil {
		return pubsub.ValidationIgnore
	}
	select {
	case <-ctx.Done():
		return pubsub.ValidationIgnore
	case <-b.ctx.Done():
		return pubsub.ValidationIgnore
	case err := <-m.Error:
		if err != nil {
			return pubsub.ValidationIgnore
		}
	}
	return pubsub.ValidationAccept
}

func (b *Broker) queueMessage(ctx context.Context, peer p2p.Peer, msg []byte) (*msgRPC, error) {
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()))
	logger.Debug("hare broker received inbound gossip message")

	// prioritize based on signature: outbound messages (self-generated) get priority
	priority := priorityq.Mid
	if peer == b.peer {
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
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.ctx.Done():
		return nil, errClosed
	case b.queueChannel <- struct{}{}:
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
		case <-b.ctx.Done():
			return
		case <-b.queueChannel:
			logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()))
			rawMsg, err := b.queue.Read()
			if err != nil {
				logger.With().Info("priority queue was closed, exiting", log.Err(err))
				return
			}
			msg, ok := rawMsg.(*msgRPC)
			if !ok {
				logger.Error("failed to convert priority queue message, ignoring")
				msg.Error <- errBadMsg
				continue
			}

			// create an inner context object to handle this message
			messageCtx := msg.Ctx

			h := types.CalcMessageHash12(msg.Data, pubsub.HareProtocol)
			msgLogger := logger.WithContext(messageCtx).WithFields(h)
			hareMsg, err := MessageFromBuffer(msg.Data)
			if err != nil {
				msgLogger.With().Error("failed to build message", h, log.Err(err))
				msg.Error <- err
				continue
			}
			msgLogger = msgLogger.WithFields(&hareMsg)

			if hareMsg.InnerMsg == nil {
				msgLogger.With().Warning("hare msg missing inner msg", log.Err(errNilInner))
				msg.Error <- errNilInner
				continue
			}
			msgLogger.Debug("broker received hare message")

			msgLayer := hareMsg.InnerMsg.Layer

			msgLogger = msgLogger.WithFields(log.Stringer("msg_layer_id", msgLayer))

			if !b.Synced(ctx, msgLayer) {
				msg.Error <- errNotSynced
				continue
			}

			isEarly := false
			if err := b.validate(messageCtx, &hareMsg); err != nil {
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
					log.Object("hare_msg", &hareMsg))
				msg.Error <- errors.New("not eligible")
				continue
			}

			// validation passed, report
			msg.Error <- nil
			msgLogger.With().Debug("broker reported hare message as valid", &hareMsg)

			if isEarly {
				b.mu.Lock()
				if _, exist := b.pending[msgLayer.Uint32()]; !exist { // create buffer if first msg
					b.pending[msgLayer.Uint32()] = make([]*Msg, 0)
				}
				b.mu.Unlock()

				// we want to write all buffered messages to a chan with InboxCapacity len
				// hence, we limit the buffer for pending messages
				b.mu.RLock()
				chCount := len(b.pending[msgLayer.Uint32()])
				b.mu.RUnlock()
				if chCount == inboxCapacity {
					msgLogger.With().Error("too many pending messages, ignoring message",
						log.Int("inbox_capacity", inboxCapacity),
						log.String("sender_id", iMsg.PubKey.ShortString()))
					continue
				}
				b.mu.Lock()
				b.pending[msgLayer.Uint32()] = append(b.pending[msgLayer.Uint32()], iMsg)
				b.mu.Unlock()
				continue
			}

			// has instance, just send
			b.mu.RLock()
			out, exist := b.outbox[msgLayer.Uint32()]
			b.mu.RUnlock()
			if !exist {
				msgLogger.With().Fatal("missing broker instance for layer", msgLayer)
			}
			msgLogger.With().Debug("broker forwarding message to outbox",
				log.Int("outbox_queue_size", len(out)))
			out <- iMsg

		case task := <-b.tasks:
			latestLayer := b.getLatestLayer()

			b.WithContext(ctx).With().Debug("broker received task, executing",
				log.Stringer("latest_layer", latestLayer))
			task()
			b.WithContext(ctx).With().Debug("broker finished executing task",
				log.Stringer("latest_layer", latestLayer))
		}
	}
}

func (b *Broker) cleanOldLayers() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := b.minDeleted.Add(1); i.Before(b.getLatestLayer()); i = i.Add(1) {
		_, exist := b.outbox[i.Uint32()]

		if !exist { // unregistered
			b.minDeleted = b.minDeleted.Add(1)
		} else { // encountered first still running layer
			break
		}
	}
}

// Register a layer to receive messages
// Note: the registering instance is assumed to be started and accepting messages.
func (b *Broker) Register(ctx context.Context, id types.LayerID) (chan *Msg, error) {
	resErr := make(chan error, 1)
	resCh := make(chan chan *Msg, 1)
	regRequest := func() {
		ctx := log.WithNewSessionID(ctx)
		b.setLatestLayer(ctx, id)

		// check to see if the node is still synced
		if b.Synced(ctx, id) {
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
				b.With().Info("unregistered layer due to maximum concurrent processes", instance)
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

	b.WithContext(ctx).With().Debug("queueing register task", id)
	b.tasks <- regRequest // send synced task

	// wait for result
	err := <-resErr
	result := <-resCh
	b.WithContext(ctx).With().Debug("register task result received", id, log.Err(err))
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
	f := func() {
		b.cleanState(id)
		b.WithContext(ctx).With().Debug("hare broker unregistered layer", id)
		wg.Done()
	}
	select {
	case b.tasks <- f:
		wg.Wait()
	case <-b.ctx.Done():
	}
}

// Synced returns true if the given layer is synced, false otherwise.
func (b *Broker) Synced(ctx context.Context, id types.LayerID) bool {
	return b.nodeSyncState.IsSynced(ctx) && b.nodeSyncState.IsBeaconSynced(id.GetEpoch())
}

// Close closes broker.
func (b *Broker) Close() {
	b.cancel()
	close(b.queueChannel)
	b.eg.Wait()
}

func (b *Broker) getLatestLayer() types.LayerID {
	b.latestLayerMu.RLock()
	defer b.latestLayerMu.RUnlock()

	return b.latestLayer
}

func (b *Broker) setLatestLayer(ctx context.Context, layer types.LayerID) {
	b.latestLayerMu.Lock()
	defer b.latestLayerMu.Unlock()

	if !layer.After(b.latestLayer) { // should expect to update only newer layers
		b.WithContext(ctx).With().Error("tried to update a previous layer",
			log.Stringer("this_layer", layer),
			log.Stringer("prev_layer", b.latestLayer))
		return
	}

	b.latestLayer = layer
}
