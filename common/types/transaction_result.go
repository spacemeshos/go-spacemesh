package types

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

// TransactionStatus ...
type TransactionStatus uint8

const (
	// TransactionSuccess is a status for successfully applied transaction.
	TransactionSuccess TransactionStatus = iota
	// TransactionFailure is a status for failed but consumed transaction.
	TransactionFailure
	// TODO(dshulyak) what about TransactionSkipped? we shouldn't store such state
	// but it might be useful to stream it.
)

// String implements human readable representation of the status.
func (t TransactionStatus) String() string {
	switch t {
	case 0:
		return "success"
	case 1:
		return "failure"
	}
	panic("unknown status")
}

// EncodeScale implements scale codec interface.
func (t TransactionStatus) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeCompact8(e, uint8(t))
}

// DecodeScale implements scale codec interface.
func (t TransactionStatus) DecodeScale(d *scale.Decoder) (uint8, int, error) {
	return scale.DecodeCompact8(d)
}

// TransactionResult is created after consuming transaction.
type TransactionResult struct {
	Status  TransactionStatus
	Message string `scale:"max=1024"`
	Gas     uint64
	Fee     uint64
	Block   BlockID
	Layer   LayerID
	// Addresses contains all updated addresses.
	Addresses []Address `scale:"max=10"`
}

// MarshalLogObject implements encoding for the tx result.
func (h *TransactionResult) MarshalLogObject(encoder log.ObjectEncoder) error {
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

// TransactionWithResult is a transaction with attached result.
type TransactionWithResult struct {
	Transaction
	TransactionResult
}
