package events

import "github.com/spacemeshos/go-spacemesh/common/types"

// streamer is the event streamer singleton.
var streamer *EventStreamer

// Stream streams an event on the streamer singleton.
func ReportNewTx(tx *types.Transaction) {
	if streamer != nil {
		streamer.channelTransaction <- tx
	}
	Publish(NewTx{
		ID:          tx.ID().String(),
		Origin:      tx.Origin().String(),
		Destination: tx.Recipient.String(),
		Amount:      tx.Amount,
		Fee:         tx.Fee})
}

func ReportValidTx(tx *types.Transaction, valid bool) {
	Publish(ValidTx{ID: tx.ID().String(), Valid: valid})
}

// Stream streams an event on the streamer singleton.
func ReportNewActivation(activation *types.ActivationTx, layersPerEpoch uint16) {
	if streamer != nil {
		streamer.channelActivation <- activation
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

func GetNewTxStream() chan *types.Transaction {
	if streamer != nil {
		return streamer.channelTransaction
	}
	return nil
}

func GetActivationStream() chan *types.ActivationTx {
	if streamer != nil {
		return streamer.channelActivation
	}
	return nil
}

// InitializeEventStream initializes the event streaming interface
func InitializeEventStream() {
	streamer = NewEventStreamer()
}

// EventStreamer is the struct that streams events to API listeners
type EventStreamer struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
}

func NewEventStreamer() *EventStreamer {
	return &EventStreamer{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
	}
}

func CloseEventStream() {
	if streamer != nil {
		close(streamer.channelTransaction)
		close(streamer.channelActivation)
		streamer = nil
	}
}
