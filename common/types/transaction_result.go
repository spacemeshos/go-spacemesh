package types

import "github.com/spacemeshos/go-spacemesh/log"

// TransactionStatus ...
type TransactionStatus uint8

const (
	// TransactionSuccess is a status for succesfully applied transaction.
	TransactionSuccess TransactionStatus = iota
	// TransactionFailure is a status for failed but consumed transaction.
	TransactionFailure
	// TODO(dshulyak) what about TransactionSkipped? we shouldn't store such state
	// but it might be useful to stream it.
)

// String implements human readable repr of the status.
func (t TransactionStatus) String() string {
	switch t {
	case 0:
		return "success"
	case 1:
		return "failure"
	}
	panic("unknown status")
}

// TransactionResult is created after consuming transaction.
type TransactionResult struct {
	Transaction Transaction
	Status      TransactionStatus
	Message     string
	Gas         uint64
	Fee         uint64
	Block       BlockID
	Layer       LayerID
	// Addresses contains all updated addresses.
	// For genesis this will be either one or two addresses.
	Addresses []Address
}

// MarshalLogObject implements encoding for the tx result.
func (h *TransactionResult) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("id", h.Transaction.ID.String())
	encoder.AddString("status", h.Status.String())
	if h.Status > 0 {
		encoder.AddString("message", h.Message)
	}
	encoder.AddUint64("gas", h.Gas)
	encoder.AddUint64("fee", h.Fee)
	encoder.AddString("block", h.Block.String())
	encoder.AddUint32("layer", h.Layer.Value)
	encoder.AddArray("addresses", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for i := range h.Addresses {
			encoder.AppendString((&h.Addresses[i]).String())
		}
		return nil
	}))
	return nil
}
