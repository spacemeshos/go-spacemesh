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
	"github.com/spacemeshos/go-spacemesh/system"
)

const inboxCapacity = 1024 // inbox size per instance

type validator interface {
	Validate(context.Context, *Msg) bool
}

// Broker is the dispatcher of incoming Hare messages.
// The broker validates that the sender is eligible and active and forwards the message to the corresponding outbox.
type Broker struct {
	log.Log
	mu             sync.RWMutex
	eValidator     validator                // provides eligibility validation
	stateQuerier   stateQuerier             // provides activeness check
	nodeSyncState  system.SyncStateProvider // provider function to check if the node is currently synced
	layersPerEpoch uint16
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

func newBroker(eValidator validator, stateQuerier stateQuerier, syncState system.SyncStateProvider, layersPerEpoch uint16, limit int, log log.Log) *Broker {
	b := &Broker{
		Log:            log,
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
func (b *Broker) HandleMessage(ctx context.Context, _ p2p.Peer, msg []byte) pubsub.ValidationResult {
	select {
	case <-ctx.Done():
		return pubsub.ValidationIgnore
	case <-b.ctx.Done():
		return pubsub.ValidationIgnore
	default:
		if err := b.handleMessage(ctx, msg); err != nil {
			return pubsub.ValidationIgnore
		}
	}
	return pubsub.ValidationAccept
}

func (b *Broker) handleMessage(ctx context.Context, msg []byte) error {
	h := types.CalcMessageHash12(msg, pubsub.HareProtocol)
	logger := b.WithContext(ctx).WithFields(log.Stringer("latest_layer", b.getLatestLayer()), h)
	hareMsg, err := MessageFromBuffer(msg)
	if err != nil {
		logger.With().Error("failed to build message", h, log.Err(err))
		return err
	}
	logger = logger.WithFields(log.Inline(&hareMsg))

	if hareMsg.InnerMsg == nil {
		logger.With().Warning("hare msg missing inner msg", log.Err(errNilInner))
		return errNilInner
	}

	logger.Debug("broker received hare message")

	msgLayer := hareMsg.InnerMsg.Layer
	if !b.Synced(ctx, msgLayer) {
		return errNotSynced
	}

	isEarly := false
	if err = b.validate(ctx, &hareMsg); err != nil {
		if !errors.Is(err, errEarlyMsg) {
			// not early, validation failed
			logger.With().Debug("broker received a message to a consensus process that is not registered",
				log.Err(err))
			return err
		}

		logger.With().Debug("early message detected", log.Err(err))
		isEarly = true
	}

	// create msg
	iMsg, err := newMsg(ctx, b.Log, hareMsg, b.stateQuerier)
	if err != nil {
		logger.With().Warning("message validation failed: could not construct msg", log.Err(err))
		return err
	}

	// validate msg
	if !b.eValidator.Validate(ctx, iMsg) {
		logger.Warning("message validation failed: eligibility validator returned false")
		return errors.New("not eligible")
	}

	// validation passed, report
	logger.With().Debug("broker reported hare message as valid")

	if isEarly {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, exist := b.pending[msgLayer.Uint32()]; !exist { // create buffer if first msg
			b.pending[msgLayer.Uint32()] = make([]*Msg, 0)
		}

		// we want to write all buffered messages to a chan with InboxCapacity len
		// hence, we limit the buffer for pending messages
		chCount := len(b.pending[msgLayer.Uint32()])
		if chCount == inboxCapacity {
			logger.With().Error("too many pending messages, ignoring message",
				log.Int("inbox_capacity", inboxCapacity),
				log.String("sender_id", iMsg.PubKey.ShortString()))
			return nil
		}
		b.pending[msgLayer.Uint32()] = append(b.pending[msgLayer.Uint32()], iMsg)
		return nil
	}

	// has instance, just send
	b.mu.RLock()
	out, exist := b.outbox[msgLayer.Uint32()]
	b.mu.RUnlock()
	if !exist {
		logger.With().Debug("missing broker instance for layer", msgLayer)
		return errTooOld
	}
	logger.With().Debug("broker forwarding message to outbox",
		log.Int("outbox_queue_size", len(out)))
	select {
	case out <- iMsg:
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errClosed
	}
	return nil
}

// listens to incoming messages and incoming tasks.
func (b *Broker) eventLoop(ctx context.Context) {
	for {
		b.WithContext(ctx).With().Debug("broker queue sizes",
			log.Int("task_queue_size", len(b.tasks)))

		select {
		case <-b.ctx.Done():
			return
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
	_ = b.eg.Wait()
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
