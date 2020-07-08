package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// reporter is the event reporter singleton.
var reporter *EventReporter

// ReportNewTx dispatches incoming events to the reporter singleton
func ReportNewTx(tx *types.Transaction) {
	if reporter != nil {
		select {
		case reporter.channelTransaction <- tx:
			log.Info("reported tx on channelTransaction")
		default:
			log.Info("not reporting tx as no one is listening on channelTransaction")
		}
	}

	Publish(NewTx{
		ID:          tx.ID().String(),
		Origin:      tx.Origin().String(),
		Destination: tx.Recipient.String(),
		Amount:      tx.Amount,
		Fee:         tx.Fee,
	})
}

// ReportValidTx reports a valid transaction
func ReportValidTx(tx *types.Transaction, valid bool) {
	Publish(ValidTx{ID: tx.ID().String(), Valid: valid})
}

// ReportNewActivation reports a new activation
func ReportNewActivation(activation *types.ActivationTx, layersPerEpoch uint16) {
	if reporter != nil {
		reporter.channelActivation <- activation
	}
	Publish(NewAtx{
		ID:      activation.ShortString(),
		LayerID: uint64(activation.PubLayerID.GetEpoch(layersPerEpoch)),
	})
}

// ReportRewardReceived reports a new reward
func ReportRewardReceived(account *types.Address, reward uint64) {
	Publish(RewardReceived{
		Coinbase: account.String(),
		Amount:   reward,
	})
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
func ReportAtxCreated(created bool, layer uint64) {
	Publish(AtxCreated{Created: created, Layer: layer})
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
func ReportNewLayer(layer *types.Layer) {
	if reporter != nil {
		reporter.channelLayer <- layer
	}
}

// ReportError reports an error
func ReportError(err NodeError) {
	if reporter != nil {
		reporter.channelError <- err
	}
}

// GetNewTxStream returns a stream of new transactions
func GetNewTxStream() chan *types.Transaction {
	if reporter != nil {
		return reporter.channelTransaction
	}
	return nil
}

// GetActivationStream returns a stream of activations
func GetActivationStream() chan *types.ActivationTx {
	if reporter != nil {
		return reporter.channelActivation
	}
	return nil
}

// GetLayerStream returns a stream of all layer data
func GetLayerStream() chan *types.Layer {
	if reporter != nil {
		return reporter.channelLayer
	}
	return nil
}

// GetErrorStream returns a stream of errors
func GetErrorStream() chan NodeError {
	if reporter != nil {
		return reporter.channelError
	}
	return nil
}

// InitializeEventReporter initializes the event reporting interface
func InitializeEventReporter(url string) {
	reporter = newEventReporter()
	if url != "" {
		InitializeEventPubsub(url)
	}
}

const (
	NodeErrorType_Error = iota
	NodeErrorType_Panic
	NodeErrorType_PanicSync
	NodeErrorType_PanicP2P
	NodeErrorType_PanicHare
	NodeErrorType_SignalShutdown
)

type NodeError struct {
	Msg   string
	Trace string
	Type  int
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
	channelLayer       chan *types.Layer
	channelError       chan NodeError
}

func newEventReporter() *EventReporter {
	return &EventReporter{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
		channelLayer:       make(chan *types.Layer),
		channelError:       make(chan NodeError),
	}
}

// CloseEventReporter shuts down the event reporting service and closes open channels
func CloseEventReporter() {
	if reporter != nil {
		close(reporter.channelTransaction)
		close(reporter.channelActivation)
		close(reporter.channelLayer)
		close(reporter.channelError)
		reporter = nil
	}
}
