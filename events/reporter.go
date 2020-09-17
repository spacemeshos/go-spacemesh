package events

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"sync"
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
		if reporter.blocking {
			reporter.channelTransaction <- txWithValidity
			log.Info("reported tx on channelTransaction: %v", txWithValidity)
		} else {
			select {
			case reporter.channelTransaction <- txWithValidity:
				log.Info("reported tx on channelTransaction: %v", txWithValidity)
			default:
				log.Info("not reporting tx as no one is listening: %v", txWithValidity)
			}
		}
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
		if reporter.blocking {
			reporter.channelActivation <- activation
			log.Info("reported activation")
		} else {
			select {
			case reporter.channelActivation <- activation:
				log.Info("reported activation")
			default:
				log.Info("not reporting activation as no one is listening")
			}
		}
	}
}

// ReportRewardReceived reports a new reward
func ReportRewardReceived(r Reward) {
	mu.RLock()
	defer mu.RUnlock()

	Publish(RewardReceived{
		Coinbase: r.Coinbase.String(),
		Amount:   r.Total,
	})

	if reporter != nil {
		if reporter.blocking {
			reporter.channelReward <- r
			log.Info("reported reward: %v", r)
		} else {
			select {
			case reporter.channelReward <- r:
				log.Info("reported reward: %v", r)
			default:
				log.Info("not reporting reward as no one is listening: %v", r)
			}
		}
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

// ReportValidBlock reports a valid block
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
		if reporter.blocking {
			reporter.channelLayer <- layer
			log.With().Info("reported new layer", layer)
		} else {
			select {
			case reporter.channelLayer <- layer:
				log.With().Info("reported new layer", layer)
			default:
				log.With().Info("not reporting new layer as no one is listening", layer)
			}
		}
	}
}

// ReportError reports an error
func ReportError(err NodeError) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if reporter.blocking {
			reporter.channelError <- err
			log.Info("reported error: %v", err)
		} else {
			select {
			case reporter.channelError <- err:
				log.Info("reported error: %v", err)
			default:
				log.Info("not reporting error as buffer is full: %v", err)
			}
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
		if reporter.blocking {
			reporter.channelStatus <- struct{}{}
			log.Info("reported status update")
		} else {
			select {
			case reporter.channelStatus <- struct{}{}:
				log.Info("reported status update")
			default:
				log.Info("not reporting status update as no one is listening")
			}
		}
	}
}

// ReportReceipt reports creation or receipt of a new tx receipt
func ReportReceipt(r TxReceipt) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if reporter.blocking {
			reporter.channelReceipt <- r
			log.Info("reported receipt: %v", r)
		} else {
			select {
			case reporter.channelReceipt <- r:
				log.Info("reported receipt: %v", r)
			default:
				log.Info("not reporting receipt as no one is listening: %v", r)
			}
		}
	}
}

// ReportAccountUpdate reports an account whose data has been updated
func ReportAccountUpdate(a types.Address) {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		if reporter.blocking {
			reporter.channelAccount <- a
			log.Info("reported account update: %v", a)
		} else {
			select {
			case reporter.channelAccount <- a:
				log.Info("reported account update: %v", a)
			default:
				log.Info("not reporting account update as no one is listening: %v", a)
			}
		}
	}
}

// GetNewTxChannel returns a channel of new transactions
func GetNewTxChannel() chan TransactionWithValidity {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelTransaction
	}
	return nil
}

// GetActivationsChannel returns a channel of activations
func GetActivationsChannel() chan *types.ActivationTx {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelActivation
	}
	return nil
}

// GetLayerChannel returns a channel of all layer data
func GetLayerChannel() chan NewLayer {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelLayer
	}
	return nil
}

// GetErrorChannel returns a channel for node errors
func GetErrorChannel() chan NodeError {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelError
	}
	return nil
}

// GetStatusChannel returns a channel for node status messages
func GetStatusChannel() chan struct{} {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelStatus
	}
	return nil
}

// GetAccountChannel returns a channel for account data updates
func GetAccountChannel() chan types.Address {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelAccount
	}
	return nil
}

// GetRewardChannel returns a channel for rewards
func GetRewardChannel() chan Reward {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelReward
	}
	return nil
}

// GetReceiptChannel returns a channel for tx receipts
func GetReceiptChannel() chan TxReceipt {
	mu.RLock()
	defer mu.RUnlock()

	if reporter != nil {
		return reporter.channelReceipt
	}
	return nil
}

// InitializeEventReporter initializes the event reporting interface
func InitializeEventReporter(url string) error {
	// By default use zero-buffer channels and non-blocking.
	return InitializeEventReporterWithOptions(url, 0, false)
}

// InitializeEventReporterWithOptions initializes the event reporting interface with
// a nonzero channel buffer. This is useful for testing, where we want reporting to
// block.
func InitializeEventReporterWithOptions(url string, bufsize int, blocking bool) error {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		return errors.New("reporter is already initialized, call CloseEventReporter before reinitializing")
	}
	reporter = newEventReporter(bufsize, blocking)
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
					log.With().Info("reporter got new layer", layer)
					ReportNodeStatusUpdate()
				case <-reporter.stopChan:
					return
				}
			}
		}()
	}
}

// The type and severity of a reported error
const (
	NodeErrorTypeError = iota
	NodeErrorTypePanic
	NodeErrorTypePanicSync
	NodeErrorTypePanicP2P
	NodeErrorTypePanicHare
	NodeErrorTypeSignalShutdown
)

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
	Type  int
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
	// TODO: We don't currently have a way to get these two.
	// See https://github.com/spacemeshos/go-spacemesh/issues/2068
	//LayerComputed
	//Smesher
}

// TransactionWithValidity wraps a tx with its validity info
type TransactionWithValidity struct {
	Transaction *types.Transaction
	Valid       bool
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	channelTransaction chan TransactionWithValidity
	channelActivation  chan *types.ActivationTx
	channelLayer       chan NewLayer
	channelError       chan NodeError
	channelStatus      chan struct{}
	channelAccount     chan types.Address
	channelReward      chan Reward
	channelReceipt     chan TxReceipt
	stopChan           chan struct{}
	blocking           bool
}

func newEventReporter(bufsize int, blocking bool) *EventReporter {
	return &EventReporter{
		channelTransaction: make(chan TransactionWithValidity, bufsize),
		channelActivation:  make(chan *types.ActivationTx, bufsize),
		channelLayer:       make(chan NewLayer, bufsize),
		channelStatus:      make(chan struct{}, bufsize),
		channelAccount:     make(chan types.Address, bufsize),
		channelReward:      make(chan Reward, bufsize),
		channelReceipt:     make(chan TxReceipt, bufsize),
		channelError:       make(chan NodeError, bufsize),
		stopChan:           make(chan struct{}),
		blocking:           blocking,
	}
}

// CloseEventReporter shuts down the event reporting service and closes open channels
func CloseEventReporter() {
	mu.Lock()
	defer mu.Unlock()
	if reporter != nil {
		close(reporter.channelTransaction)
		close(reporter.channelActivation)
		close(reporter.channelLayer)
		close(reporter.channelError)
		close(reporter.channelStatus)
		close(reporter.channelAccount)
		close(reporter.channelReward)
		close(reporter.channelReceipt)
		close(reporter.stopChan)
		reporter = nil
	}
}
