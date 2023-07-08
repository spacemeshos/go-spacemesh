package events

import (
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Subscription is a subscription to events.
// Consumer must be aware that publish will block if subsription is not read fast enough.
type Subscription = event.Subscription

var (
	mu sync.RWMutex
	// reporter is the event reporter singleton.
	reporter *EventReporter
)

// InitializeReporter initializes the event reporting interface with
// a nonzero channel buffer. This is useful for testing, where we want reporting to
// block.
func InitializeReporter() {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		return
	}
	reporter = newEventReporter()
}

// EventHook returns hook for logger.
func EventHook() func(entry zapcore.Entry) error {
	return func(entry zapcore.Entry) error {
		// If we report anything less than this we'll end up in an infinite loop
		if entry.Level >= zapcore.ErrorLevel {
			ReportError(NodeError{
				Msg:   entry.Message,
				Trace: string(debug.Stack()),
				Level: entry.Level,
			})
		}
		return nil
	}
}

// ReportNewTx dispatches incoming events to the reporter singleton.
func ReportNewTx(layerID types.LayerID, tx *types.Transaction) {
	ReportTxWithValidity(layerID, tx, true)
}

// ReportTxWithValidity reports a tx along with whether it was just invalidated.
func ReportTxWithValidity(layerID types.LayerID, tx *types.Transaction, valid bool) {
	mu.RLock()
	defer mu.RUnlock()
	txWithValidity := Transaction{
		Transaction: tx,
		LayerID:     layerID,
		Valid:       valid,
	}
	if reporter != nil {
		if err := reporter.transactionEmitter.Emit(txWithValidity); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit transaction", tx.ID, layerID, log.Err(err))
		} else {
			log.Debug("reported tx: %v", txWithValidity)
		}
	}
}

// ReportNewActivation reports a new activation.
func ReportNewActivation(activation *types.VerifiedActivationTx) {
	mu.RLock()
	defer mu.RUnlock()

	activationTxEvent := ActivationTx{VerifiedActivationTx: activation}
	if reporter != nil {
		if err := reporter.activationEmitter.Emit(activationTxEvent); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit activation", activation.ID(), activation.PublishEpoch, log.Err(err))
		}
	}
}

// ReportRewardReceived reports a new reward.
func ReportRewardReceived(r Reward) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if err := reporter.rewardEmitter.Emit(r); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit rewards", r.Layer, log.Err(err))
		} else {
			log.Debug("reported reward: %v", r)
		}
	}
}

// ReportLayerUpdate reports a new layer, or an update to an existing layer.
func ReportLayerUpdate(layer LayerUpdate) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if err := reporter.layerEmitter.Emit(layer); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit updated layer", layer, log.Err(err))
		} else {
			log.With().Debug("reported new or updated layer", layer)
		}
	}
}

// ReportError reports an error.
func ReportError(err NodeError) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if err := reporter.errorEmitter.Emit(err); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit error", log.Err(err))
		} else {
			log.Debug("reported error: %v", err)
		}
	}
}

// ReportNodeStatusUpdate reports an update to the node status. It just
// pings the listener to notify them that there is an update; the listener
// is responsible for fetching the new status details. This is because
// status contains disparate information coming from different services,
// and the listener already knows how to gather that information so there
// is no point in duplicating that logic here.
// Note: There is some overlap with channelLayer here, as a new latest
// or verified layer should be sent over that channel as well. However,
// that happens inside the Mesh, at the source. It doesn't currently
// happen here because the status update includes only a layer ID, not
// full layer data, and the Reporter currently has no way to retrieve
// full layer data.
func ReportNodeStatusUpdate() {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if err := reporter.statusEmitter.Emit(Status{}); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit status update", log.Err(err))
		} else {
			log.Debug("reported status update")
		}
	}
}

// ReportResult reports creation or receipt of a new tx receipt.
func ReportResult(rst types.TransactionWithResult) {
	if reporter != nil {
		if err := reporter.resultsEmitter.Emit(rst); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit tx results", rst.ID, log.Err(err))
		}
	}
}

// ReportAccountUpdate reports an account whose data has been updated.
func ReportAccountUpdate(a types.Address) {
	mu.RLock()
	defer mu.RUnlock()

	accountEvent := Account{Address: a}

	if reporter != nil {
		if err := reporter.accountEmitter.Emit(accountEvent); err != nil {
			// TODO(nkryuchkov): consider returning an error and log outside the function
			log.With().Error("Failed to emit account update", log.String("account", a.String()), log.Err(err))
		} else {
			log.With().Debug("reported account update", a)
		}
	}
}

// SubscribeTxs subscribes to new transactions.
func SubscribeTxs() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(Transaction))
		if err != nil {
			log.With().Panic("Failed to subscribe to transactions")
		}

		return sub
	}
	return nil
}

// SubscribeActivations subscribes to activations.
func SubscribeActivations() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(ActivationTx))
		if err != nil {
			log.With().Panic("Failed to subscribe to activations")
		}

		return sub
	}
	return nil
}

// SubscribeLayers subscribes to all layer data.
func SubscribeLayers() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(LayerUpdate))
		if err != nil {
			log.With().Panic("Failed to subscribe to layers")
		}

		return sub
	}
	return nil
}

// SubscribeErrors subscribes to node errors.
func SubscribeErrors() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(NodeError))
		if err != nil {
			log.With().Panic("Failed to subscribe to errors")
		}

		return sub
	}
	return nil
}

// SubscribeStatus subscribes to node status messages.
func SubscribeStatus() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(Status))
		if err != nil {
			log.With().Panic("Failed to subscribe to status")
		}

		return sub
	}
	return nil
}

// SubscribeAccount subscribes to account data updates.
func SubscribeAccount() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(Account))
		if err != nil {
			log.With().Panic("Failed to subscribe to account updates")
		}

		return sub
	}
	return nil
}

// SubscribeRewards subscribes to rewards.
func SubscribeRewards() Subscription {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		sub, err := reporter.bus.Subscribe(new(Reward))
		if err != nil {
			log.With().Panic("Failed to subscribe to rewards")
		}

		return sub
	}
	return nil
}

// SubscribeToLayers is used to track and report automatically every time a
// new layer is reached.
func SubscribeToLayers(ticker LayerClock) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter == nil {
		return
	}

	// This will block, so run in a goroutine
	go func() {
		next := ticker.CurrentLayer().Add(1)
		for {
			mu.RLock()
			stopChan := reporter.stopChan
			mu.RUnlock()

			select {
			case <-ticker.AwaitLayer(next):
				current := ticker.CurrentLayer()
				if current.Before(next) {
					log.Info("time sync detected, realigning ProposalBuilder")
					continue
				}
				next = current.Add(1)
				log.With().Debug("reporter got new layer", current)
				ReportNodeStatusUpdate()
			case <-stopChan:
				return
			}
		}
	}()
}

func SubscribeUserEvents(opts ...SubOpt) (*BufferedSubscription[UserEvent], *Ring[UserEvent], error) {
	mu.RLock()
	defer mu.RUnlock()
	if reporter == nil {
		return nil, nil, nil
	}
	return reporter.SubUserEvents(opts...)
}

// The status of a layer
// TODO: this list is woefully inadequate and does not map to reality. See https://github.com/spacemeshos/api/issues/144.
const (
	LayerStatusTypeUnknown   = iota
	LayerStatusTypeApproved  // approved by Hare
	LayerStatusTypeConfirmed // confirmed by Tortoise
	LayerStatusTypeApplied   // applied to state
)

// LayerUpdate packages up a layer with its status (which a layer does not ordinarily contain).
type LayerUpdate struct {
	LayerID types.LayerID
	Status  int
}

// Field returns a log field. Implements the LoggableField interface.
func (nl LayerUpdate) Field() log.Field {
	return log.String("layer", fmt.Sprintf("status: %d, number: %d", nl.Status, nl.LayerID))
}

// NodeError represents an internal error to be reported.
type NodeError struct {
	Msg   string
	Trace string
	Level zapcore.Level
}

// TxReceipt represents a transaction receipt.
type TxReceipt struct {
	ID      types.TransactionID
	Result  int
	GasUsed uint64
	Fee     uint64
	Layer   types.LayerID
	Index   uint32
	Address types.Address
}

// Reward represents a reward object with extra data needed by the API.
type Reward struct {
	Layer       types.LayerID
	Total       uint64
	LayerReward uint64
	Coinbase    types.Address
}

// Transaction wraps a tx with its layer ID and validity info.
type Transaction struct {
	Transaction *types.Transaction
	LayerID     types.LayerID
	Valid       bool
}

// ActivationTx wraps *types.VerifiedActivationTx.
type ActivationTx struct {
	*types.VerifiedActivationTx
}

// Status indicates status change event.
type Status struct{}

// Account wraps account address.
type Account struct {
	types.Address
}

// EventReporter is the struct that receives incoming events and dispatches them.
type EventReporter struct {
	bus                event.Bus
	transactionEmitter event.Emitter
	activationEmitter  event.Emitter
	layerEmitter       event.Emitter
	errorEmitter       event.Emitter
	statusEmitter      event.Emitter
	accountEmitter     event.Emitter
	rewardEmitter      event.Emitter
	resultsEmitter     event.Emitter
	proposalsEmitter   event.Emitter
	events             struct {
		sync.Mutex
		buf     *Ring[UserEvent]
		emitter event.Emitter
	}
	stopChan chan struct{}
}

func (r *EventReporter) EmitUserEvent(ev UserEvent) error {
	r.events.Lock()
	defer r.events.Unlock()
	r.events.buf.insert(ev)
	return r.events.emitter.Emit(ev)
}

func (r *EventReporter) SubUserEvents(opts ...SubOpt) (*BufferedSubscription[UserEvent], *Ring[UserEvent], error) {
	r.events.Lock()
	defer r.events.Unlock()
	sub, err := Subscribe[UserEvent](opts...)
	if err != nil {
		return nil, nil, err
	}
	buf := r.events.buf.Copy()
	return sub, buf, nil
}

func newEventReporter() *EventReporter {
	bus := eventbus.NewBus()
	transactionEmitter, err := bus.Emitter(new(Transaction))
	if err != nil {
		log.With().Panic("failed to create transaction emitter", log.Err(err))
	}
	activationEmitter, err := bus.Emitter(new(ActivationTx))
	if err != nil {
		log.With().Panic("failed to create activation emitter", log.Err(err))
	}
	layerEmitter, err := bus.Emitter(new(LayerUpdate))
	if err != nil {
		log.With().Panic("failed to create layer emitter", log.Err(err))
	}
	statusEmitter, err := bus.Emitter(new(Status))
	if err != nil {
		log.With().Panic("failed to create status emitter", log.Err(err))
	}
	accountEmitter, err := bus.Emitter(new(Account))
	if err != nil {
		log.With().Panic("failed to create account emitter", log.Err(err))
	}
	rewardEmitter, err := bus.Emitter(new(Reward))
	if err != nil {
		log.With().Panic("failed to create reward emitter", log.Err(err))
	}
	resultsEmitter, err := bus.Emitter(new(types.TransactionWithResult))
	if err != nil {
		log.With().Panic("failed to create receipt emitter", log.Err(err))
	}
	errorEmitter, err := bus.Emitter(new(NodeError))
	if err != nil {
		log.With().Panic("failed to create error emitter", log.Err(err))
	}
	proposalsEmitter, err := bus.Emitter(new(EventProposal))
	if err != nil {
		log.With().Panic("failed to to create proposal emitter", log.Err(err))
	}
	eventsEmitter, err := bus.Emitter(new(UserEvent))
	if err != nil {
		log.With().Panic("failed to to create proposal emitter", log.Err(err))
	}

	reporter := &EventReporter{
		bus:                bus,
		transactionEmitter: transactionEmitter,
		activationEmitter:  activationEmitter,
		layerEmitter:       layerEmitter,
		statusEmitter:      statusEmitter,
		accountEmitter:     accountEmitter,
		rewardEmitter:      rewardEmitter,
		resultsEmitter:     resultsEmitter,
		errorEmitter:       errorEmitter,
		proposalsEmitter:   proposalsEmitter,
		stopChan:           make(chan struct{}),
	}
	reporter.events.buf = newRing[UserEvent](100)
	reporter.events.emitter = eventsEmitter
	return reporter
}

// CloseEventReporter shuts down the event reporting service and closes open channels.
func CloseEventReporter() {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		if err := reporter.transactionEmitter.Close(); err != nil {
			log.With().Panic("failed to close transactionEmitter", log.Err(err))
		}
		if err := reporter.activationEmitter.Close(); err != nil {
			log.With().Panic("failed to close activationEmitter", log.Err(err))
		}
		if err := reporter.layerEmitter.Close(); err != nil {
			log.With().Panic("failed to close layerEmitter", log.Err(err))
		}
		if err := reporter.errorEmitter.Close(); err != nil {
			log.With().Panic("failed to close errorEmitter", log.Err(err))
		}
		if err := reporter.statusEmitter.Close(); err != nil {
			log.With().Panic("failed to close statusEmitter", log.Err(err))
		}
		if err := reporter.accountEmitter.Close(); err != nil {
			log.With().Panic("failed to close accountEmitter", log.Err(err))
		}
		if err := reporter.rewardEmitter.Close(); err != nil {
			log.With().Panic("failed to close rewardEmitter", log.Err(err))
		}
		if err := reporter.resultsEmitter.Close(); err != nil {
			log.With().Panic("failed to close receiptEmitter", log.Err(err))
		}
		if err := reporter.proposalsEmitter.Close(); err != nil {
			log.With().Panic("failed to close propoposalsEmitter", log.Err(err))
		}

		close(reporter.stopChan)
		reporter = nil
	}
}

func newRing[T any](size int) *Ring[T] {
	return &Ring[T]{
		last: -1,
		data: make([]T, size),
	}
}

// Ring is an insert only buffer.
type Ring[T any] struct {
	data        []T
	first, last int
}

func (r *Ring[T]) insert(value T) {
	last := r.last
	r.last++
	r.last %= len(r.data)
	r.data[r.last] = value
	if last != -1 && r.first == r.last {
		r.first++
		r.first %= len(r.data)
	}
}

func (r *Ring[T]) Copy() *Ring[T] {
	cp := &Ring[T]{
		first: r.first,
		last:  r.last,
		data:  make([]T, r.Len()),
	}
	copy(cp.data, r.data)
	return cp
}

func (r *Ring[T]) Len() int {
	if r.last == -1 {
		return 0
	}
	if r.first > r.last {
		return len(r.data)
	}
	return r.last - r.first + 1
}

func (r *Ring[T]) Iterate(iter func(val T) bool) {
	if r.last == -1 {
		return
	}
	if r.last > r.first {
		for i := r.first; i <= r.last; i++ {
			if !iter(r.data[i]) {
				return
			}
		}
		return
	}
	for i := r.first; i < len(r.data); i++ {
		if !iter(r.data[i]) {
			return
		}
	}
	for i := 0; i < r.first; i++ {
		if !iter(r.data[i]) {
			return
		}
	}
}
