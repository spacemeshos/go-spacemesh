package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
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
			log.Info("not reporting tx as no one is listening")
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
		select {
		case reporter.channelActivation <- activation:
			log.Info("reported activation")
		default:
			log.Info("not reporting activation as no one is listening")
		}
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
func ReportNewLayer(layer NewLayer) {
	if reporter != nil {
		select {
		case reporter.channelLayer <- layer:
			log.Info("reported layer")
		default:
			log.Info("not reporting layer as no one is listening")
		}
	}
}

// ReportError reports an error
func ReportError(err NodeError) {
	if reporter != nil {
		select {
		case reporter.channelError <- err:
			log.Info("reported error: %v", err)
		default:
			log.Info("not reporting error as buffer is full: %v", err)
		}
	}
}

// ReportNodeStatus reports an update to the node status
// Note: There is some overlap with channelLayer here, as a new latest
// or verified layer should be sent over that channel as well. However,
// that happens inside the Mesh, at the source. It doesn't currently
// happen here because the status update includes only a layer ID, not
// full layer data, and the Reporter currently has no way to retrieve
// full layer data.
func ReportNodeStatus(setters ...SetStatusElem) {
	if reporter != nil {
		// Note that we make no attempt to remove duplicate status messages
		// from the stream, so the same status may be reported several times.
		for _, setter := range setters {
			setter(&reporter.lastStatus)
		}
		select {
		case reporter.channelStatus <- reporter.lastStatus:
			log.Info("reported status: %v", reporter.lastStatus)
		default:
			log.Info("not reporting status as no one is listening: %v", reporter.lastStatus)
		}
	}
}

// GetNewTxChannel returns a channel of new transactions
func GetNewTxChannel() chan *types.Transaction {
	if reporter != nil {
		return reporter.channelTransaction
	}
	return nil
}

// GetActivationsChannel returns a channel of activations
func GetActivationsChannel() chan *types.ActivationTx {
	if reporter != nil {
		return reporter.channelActivation
	}
	return nil
}

// GetLayerChannel returns a channel of all layer data
func GetLayerChannel() chan NewLayer {
	if reporter != nil {
		return reporter.channelLayer
	}
	return nil
}

// GetErrorChannel returns a channel for node errors
func GetErrorChannel() chan NodeError {
	if reporter != nil {
		return reporter.channelError
	}
	return nil
}

// GetStatusChannel returns a channel for node status messages
func GetStatusChannel() chan NodeStatus {
	if reporter != nil {
		return reporter.channelStatus
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

// SubscribeToLayers is used to track and report automatically every time a
// new layer is reached.
func SubscribeToLayers(newLayerCh timesync.LayerTimer) {
	if reporter != nil {
		// This will block, so run in a goroutine
		go func() {
			for {
				select {
				case layer := <-newLayerCh:
					log.With().Info("reporter got new layer", layer)
					ReportNodeStatus(LayerCurrent(layer))
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

// NodeError represents an internal error to be reported
type NodeError struct {
	Msg   string
	Trace string
	Type  int
}

// NodeStatus represents the current status of the node, to be reported
type NodeStatus struct {
	NumPeers      uint64
	IsSynced      bool
	LayerSynced   types.LayerID
	LayerCurrent  types.LayerID
	LayerVerified types.LayerID
}

// SetStatusElem sets a status
type SetStatusElem func(*NodeStatus)

// NumPeers sets peers
func NumPeers(n uint64) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.NumPeers = n
	}
}

// IsSynced sets if synced
func IsSynced(synced bool) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.IsSynced = synced
	}
}

// LayerSynced sets whether a layer is synced
func LayerSynced(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerSynced = lid
	}
}

// LayerCurrent sets current layer
func LayerCurrent(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerCurrent = lid
	}
}

// LayerVerified sets verified layer
func LayerVerified(lid types.LayerID) SetStatusElem {
	return func(ns *NodeStatus) {
		ns.LayerVerified = lid
	}
}

// EventReporter is the struct that receives incoming events and dispatches them
type EventReporter struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
	channelLayer       chan NewLayer
	channelError       chan NodeError
	channelStatus      chan NodeStatus
	lastStatus         NodeStatus
	stopChan           chan struct{}
}

func newEventReporter() *EventReporter {
	return &EventReporter{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
		channelLayer:       make(chan NewLayer),
		channelStatus:      make(chan NodeStatus),
		lastStatus:         NodeStatus{},
		stopChan:           make(chan struct{}),

		// In general, we use unbuffered channels, since we expect data to be consumed
		// as fast as we produce it, and we don't expect many data items to be created
		// and reported in rapid succession. Also, it's not such a big deal if we
		// miss something since it can always be queried again later. But errors are a
		// bit of a special case. Sometimes one error triggers another in rapid
		// succession. So as not to lose these, we use a buffered channel here.
		channelError: make(chan NodeError, 100),
	}
}

// CloseEventReporter shuts down the event reporting service and closes open channels
func CloseEventReporter() {
	if reporter != nil {
		close(reporter.channelTransaction)
		close(reporter.channelActivation)
		close(reporter.channelLayer)
		close(reporter.channelError)
		close(reporter.channelStatus)
		close(reporter.stopChan)
		reporter = nil
	}
}
