package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// reporter is the event reporter singleton.
var reporter *EventReporter

// Stream streams an event on the reporter singleton.
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

func ReportValidTx(tx *types.Transaction, valid bool) {
	Publish(ValidTx{ID: tx.ID().String(), Valid: valid})
}

// Stream streams an event on the reporter singleton.
func ReportNewActivation(activation *types.ActivationTx, layersPerEpoch uint16) {
	if reporter != nil {
		reporter.channelActivation <- activation
	}
	Publish(NewAtx{
		ID:      activation.ShortString(),
		LayerID: uint64(activation.PubLayerID.GetEpoch(layersPerEpoch)),
	})
}

func ReportRewardReceived(account *types.Address, reward uint64) {
	Publish(RewardReceived{
		Coinbase: account.String(),
		Amount:   reward,
	})
}

func ReportNewBlock(blk *types.Block) {
	Publish(NewBlock{
		ID:    blk.ID().String(),
		Atx:   blk.ATXID.ShortString(),
		Layer: uint64(blk.LayerIndex),
	})
}

func ReportValidBlock(blockID types.BlockID, valid bool) {
	Publish(ValidBlock{
		ID:    blockID.String(),
		Valid: valid,
	})
}

func ReportAtxCreated(created bool, layer uint64) {
	Publish(AtxCreated{Created: created, Layer: layer})
}

func ReportValidActivation(activation *types.ActivationTx, valid bool) {
	Publish(ValidAtx{ID: activation.ShortString(), Valid: valid})
}

func ReportDoneCreatingBlock(eligible bool, layer uint64, error string) {
	Publish(DoneCreatingBlock{
		Eligible: eligible,
		Layer:    layer,
		Error:    error,
	})
}

func ReportNewLayer(layer *types.Layer) {
	if reporter != nil {
		reporter.channelLayer <- layer
	}
}

func GetNewTxStream() chan *types.Transaction {
	if reporter != nil {
		return reporter.channelTransaction
	}
	return nil
}

func GetActivationStream() chan *types.ActivationTx {
	if reporter != nil {
		return reporter.channelActivation
	}
	return nil
}

func GetLayerStream() chan *types.Layer {
	if reporter != nil {
		return reporter.channelLayer
	}
	return nil
}

// InitializeEventReporter initializes the event reporting interface
func InitializeEventReporter() {
	reporter = NewEventReporter()
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
	channelLayer       chan *types.Layer
}

func NewEventReporter() *EventReporter {
	return &EventReporter{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
		channelLayer:       make(chan *types.Layer),
	}
}

func CloseEventReporter() {
	if reporter != nil {
		close(reporter.channelTransaction)
		close(reporter.channelActivation)
		close(reporter.channelLayer)
		reporter = nil
	}
}
