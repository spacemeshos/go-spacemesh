package events

import (
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"go.uber.org/zap/zapcore"
)

// reporter is the event reporter singleton.
var reporter *EventReporter

// we use a mutex to ensure thread safety
var mu sync.RWMutex

func init() {
	mu = sync.RWMutex{}
}

// ReportNewTx dispatches incoming events to the reporter singleton
func ReportNewTx(tx *types.Transaction) {
	Publish(NewTx{
		ID:          tx.ID().String(),
		Origin:      tx.Origin().String(),
		Destination: tx.Recipient.String(),
		Amount:      tx.Amount,
		Fee:         tx.Fee,
	})
	ReportTxWithValidity(tx, true)
}

// ReportTxWithValidity reports a tx along with whether it was just invalidated
func ReportTxWithValidity(tx *types.Transaction, valid bool) {
	mu.RLock()
	defer mu.RUnlock()
	txWithValidity := TransactionWithValidity{
		Transaction: tx,
		Valid:       valid,
	}
	if reporter != nil {
		log.Info("about to report tx validity updates for : %v", txWithValidity)
		for sub := range reporter.transactionSubs {
			select {
			case sub <- txWithValidity:
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported tx validity update to subscribers: %v", txWithValidity)
	}
}

// ReportValidTx reports a valid transaction
func ReportValidTx(tx *types.Transaction, valid bool) {
	Publish(ValidTx{ID: tx.ID().String(), Valid: valid})
}

// ReportNewActivation reports a new activation
func ReportNewActivation(activation *types.ActivationTx) {
	mu.RLock()
	defer mu.RUnlock()

	Publish(NewAtx{
		ID:      activation.ShortString(),
		LayerID: uint64(activation.PubLayerID.GetEpoch()),
	})

	if reporter != nil {
		innerBytes, err := activation.InnerBytes()
		if err != nil {
			log.Error("error attempting to report activation: unable to encode activation")
			return
		}
		for sub := range reporter.activationSubs {
			select {
			case sub <- activation:
				log.With().Debug("reported activation", activation.Fields(len(innerBytes))...)
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}

	}
}

// SubscribeToRewards subscribes a channel to rewards events
func SubscribeToRewards() chan Reward {
	mu.RLock()
	defer mu.RUnlock()
	if reporter != nil {
		newChan := make(chan Reward, reporter.bufsize)
		reporter.rewardsSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromRewards unsubscribes a channel from rewards events
// do we need to close the subscriber channel here?
func UnsubscribeFromRewards(subscriptionChannel chan Reward) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.rewardsSubs[subscriptionChannel]; exists {
			delete(reporter.rewardsSubs, subscriptionChannel)
		}
	}
}

// ReportRewardReceived reports a new reward
func ReportRewardReceived(r Reward) {
	mu.RLock()
	defer mu.RUnlock()

	Publish(RewardReceived{
		Coinbase:  r.Coinbase.String(),
		Amount:    r.Total,
		SmesherID: r.Smesher.ToBytes(),
	})

	if reporter != nil {
		log.Info("about to report reward: %v", r)
		for sub := range reporter.rewardsSubs {
			select {
			case sub <- r:
				log.Info("reporter send reward %v to subscriber", r)
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported reward to subscriber: %v", r)
	}
}

// ReportNewBlock reports a new block
func ReportNewBlock(blk *types.Block) {
	Publish(NewBlock{
		ID:    blk.ID().String(),
		Atx:   blk.ATXID.ShortString(),
		Layer: uint64(blk.LayerIndex),
	})
}

// ReportValidBlock reports a block's validity
func ReportValidBlock(blockID types.BlockID, valid bool) {
	Publish(ValidBlock{
		ID:    blockID.String(),
		Valid: valid,
	})
}

// ReportAtxCreated reports a created activation
func ReportAtxCreated(created bool, layer uint64, id string) {
	Publish(AtxCreated{Created: created, Layer: layer, ID: id})
}

// ReportValidActivation reports a valid activation
func ReportValidActivation(activation *types.ActivationTx, valid bool) {
	Publish(ValidAtx{ID: activation.ShortString(), Valid: valid})
}

// ReportDoneCreatingBlock reports a created block
func ReportDoneCreatingBlock(eligible bool, layer uint64, error string) {
	Publish(DoneCreatingBlock{
		Eligible: eligible,
		Layer:    layer,
		Error:    error,
	})
}

// ReportNewLayer reports a new layer
func ReportNewLayer(layer NewLayer) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		log.Info("about to report new layer : %v", layer)
		for sub := range reporter.layerSubs {
			select {
			case sub <- layer:
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported layer update to subscribers: %v", layer)
	}
}

// ReportError reports an error
func ReportError(err NodeError) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		log.Info("about to report error updates for error : %v", err)
		for sub := range reporter.errorSubs {
			select {
			case sub <- err:
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported error update to subscribers: %v", err)
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
		for sub := range reporter.statusSubs {
			select {
			case sub <- struct{}{}:
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported status update to subscribers: %v")
	}
}

// ReportReceipt reports creation or receipt of a new tx receipt
func ReportReceipt(r TxReceipt) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		log.Info("about to report receipts for: %v", r)
		for sub := range reporter.receiptsSubs {
			select {
			case sub <- r:
				log.Info("reporter send receipt %v to subscriber", r)
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported receipt to subscribers: %v", r)
	}
}

// ReportAccountUpdate reports an account whose data has been updated
func ReportAccountUpdate(a types.Address) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		log.Info("about to report account update for: %v", a)
		for sub := range reporter.accountsSubs {
			select {
			case sub <- a:
				log.Info("reporter send account %v to subscriber", a)
			default:
				log.Debug("reporter would block on subscriber %v", sub)
			}
		}
		log.Info("reported account update to subscribers: %v", a)
	}
}

// GetNewTxChannel returns a channel of new transactions
// func GetNewTxChannel() chan TransactionWithValidity {
// 	mu.RLock()
// 	defer mu.RUnlock()

// 	if reporter != nil {
// 		return reporter.channelTransaction
// 	}
// 	return nil
// }

// SubscribeToTxChannel subscribes a process to tx updates
func SubscribeToTxChannel() chan TransactionWithValidity {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan TransactionWithValidity, reporter.bufsize)
		reporter.transactionSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromTxChannel unsubscribes a process from tx updates
func UnsubscribeFromTxChannel(subscriptionChannel chan TransactionWithValidity) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.transactionSubs[subscriptionChannel]; exists {
			delete(reporter.transactionSubs, subscriptionChannel)
		}
	}
}

// SubscribeToActivations allows a process to subscribe to events of activations
func SubscribeToActivations() chan *types.ActivationTx {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan *types.ActivationTx, reporter.bufsize)
		reporter.activationSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromActivations allows a process to unsubscribe from layer events
func UnsubscribeFromActivations(subscriptionChannel chan *types.ActivationTx) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.activationSubs[subscriptionChannel]; exists {
			delete(reporter.activationSubs, subscriptionChannel)
		}
	}
}

// SubscribeToLayerChannel allows a process to subscribe to events of newLayers
func SubscribeToLayerChannel() chan NewLayer {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan NewLayer, reporter.bufsize)
		reporter.layerSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromLayerChannel allows a process to unsubscribe from layer events
func UnsubscribeFromLayerChannel(subscriptionChannel chan NewLayer) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.layerSubs[subscriptionChannel]; exists {
			delete(reporter.layerSubs, subscriptionChannel)
		}
	}
}

// SubscribeToErrors allows a goroutine to receive a channel for
// error notifications
func SubscribeToErrors() chan NodeError {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan NodeError, reporter.bufsize)
		reporter.errorSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromErrors allows a goroutine to unsubscribe from the error channel
func UnsubscribeFromErrors(subscriberChannel chan NodeError) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.errorSubs[subscriberChannel]; exists {
			delete(reporter.errorSubs, subscriberChannel)
		}
	}
}

// SubscribeToStatus subscribes a channel to receive status updates
func SubscribeToStatus() chan struct{} {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan struct{}, reporter.bufsize)
		reporter.statusSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromStatus unsubscribes a channel from receiving status updates
func UnsubscribeFromStatus(subscriberChannel chan struct{}) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.statusSubs[subscriberChannel]; exists {
			delete(reporter.statusSubs, subscriberChannel)
			return
		}
	}
}

// SubscribeToAccounts subscribes a channel to account update events
func SubscribeToAccounts() chan types.Address {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan types.Address, reporter.bufsize)
		reporter.accountsSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromAccounts unsubscribes a channel from accounts events
// do we need to close the subscriber channel here?
func UnsubscribeFromAccounts(subscriptionChannel chan types.Address) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.accountsSubs[subscriptionChannel]; exists {
			delete(reporter.accountsSubs, subscriptionChannel)
		}
	}
}

// SubscribeToReceipts allows a process to subscribe to receipt events
func SubscribeToReceipts() chan TxReceipt {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		newChan := make(chan TxReceipt, reporter.bufsize)
		reporter.receiptsSubs[newChan] = struct{}{}
		return newChan
	}
	return nil
}

// UnsubscribeFromReceipts removes a channel from receipt event notifications
func UnsubscribeFromReceipts(subscriptionChannel chan TxReceipt) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if _, exists := reporter.receiptsSubs[subscriptionChannel]; exists {
			delete(reporter.receiptsSubs, subscriptionChannel)
		}
	}
}

// InitializeEventReporter initializes the event reporting interface
func InitializeEventReporter(url string) error {
	// By default use zero-buffer channels and non-blocking.
	return InitializeEventReporterWithOptions(url, 0)
}

// InitializeEventReporterWithOptions initializes the event reporting interface with
// a nonzero channel buffer. This is useful for testing, where we want reporting to
// block.
func InitializeEventReporterWithOptions(url string, bufsize int) error {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		return errors.New("reporter is already initialized, call CloseEventReporter before reinitializing")
	}
	reporter = newEventReporter(bufsize)
	if url != "" {
		InitializeEventPubsub(url)
	}
	return nil
}

// SubscribeToLayers is used to track and report automatically every time a
// new layer is reached.
func SubscribeToLayers(newLayerCh timesync.LayerTimer) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		// This will block, so run in a goroutine
		go func() {
			for {
				select {
				case layer := <-newLayerCh:
					log.With().Debug("reporter got new layer", layer)
					ReportNodeStatusUpdate()
				case <-reporter.stopChan:
					return
				}
			}
		}()
	}
}

// The status of a layer
const (
	LayerStatusTypeUnknown   = iota
	LayerStatusTypeApproved  // approved by Hare
	LayerStatusTypeConfirmed // confirmed by Tortoise
)

// NewLayer packages up a layer with its status (which a layer does not
// ordinarily contain)
type NewLayer struct {
	Layer  *types.Layer
	Status int
}

// Field returns a log field. Implements the LoggableField interface.
func (nl NewLayer) Field() log.Field {
	return log.String("layer",
		fmt.Sprintf("status: %d, number: %d, numblocks: %d",
			nl.Status, nl.Layer.Index(), len(nl.Layer.Blocks())))
}

// NodeError represents an internal error to be reported
type NodeError struct {
	Msg   string
	Trace string
	Level zapcore.Level
}

// TxReceipt represents a transaction receipt
type TxReceipt struct {
	ID      types.TransactionID
	Result  int
	GasUsed uint64
	Fee     uint64
	Layer   types.LayerID
	Index   uint32
	Address types.Address
}

// Reward represents a reward object with extra data needed by the API
type Reward struct {
	Layer       types.LayerID
	Total       uint64
	LayerReward uint64
	Coinbase    types.Address
	// TODO: We don't currently have a way to get the Layer Computed.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2275
	//LayerComputed
	Smesher types.NodeID
}

// TransactionWithValidity wraps a tx with its validity info
type TransactionWithValidity struct {
	Transaction *types.Transaction
	Valid       bool
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	stopChan        chan struct{}
	rewardsSubs     map[chan Reward]struct{}
	accountsSubs    map[chan types.Address]struct{}
	receiptsSubs    map[chan TxReceipt]struct{}
	statusSubs      map[chan struct{}]struct{}
	errorSubs       map[chan NodeError]struct{}
	layerSubs       map[chan NewLayer]struct{}
	activationSubs  map[chan *types.ActivationTx]struct{}
	transactionSubs map[chan TransactionWithValidity]struct{}
	bufsize         int
}

func newEventReporter(bufsize int) *EventReporter {
	return &EventReporter{
		stopChan:        make(chan struct{}),
		rewardsSubs:     make(map[chan Reward]struct{}),
		accountsSubs:    make(map[chan types.Address]struct{}),
		receiptsSubs:    make(map[chan TxReceipt]struct{}),
		statusSubs:      make(map[chan struct{}]struct{}),
		errorSubs:       make(map[chan NodeError]struct{}),
		layerSubs:       make(map[chan NewLayer]struct{}),
		activationSubs:  make(map[chan *types.ActivationTx]struct{}),
		transactionSubs: make(map[chan TransactionWithValidity]struct{}),
		bufsize:         bufsize,
	}
}

// CloseEventReporter shuts down the event reporting service and closes open channels
func CloseEventReporter() {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		close(reporter.stopChan)
		for c := range reporter.rewardsSubs {
			close(c)
		}
		for c := range reporter.accountsSubs {
			close(c)
		}
		for c := range reporter.receiptsSubs {
			close(c)
		}
		for c := range reporter.statusSubs {
			close(c)
		}
		for c := range reporter.errorSubs {
			close(c)
		}
		for c := range reporter.layerSubs {
			close(c)
		}
		for c := range reporter.activationSubs {
			close(c)
		}
		for c := range reporter.transactionSubs {
			close(c)
		}
		reporter = nil
	}
}
