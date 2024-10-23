package core

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const TxSizeLimit = 1024

type (
	// PublicKey is an alias to Hash32.
	PublicKey = types.Hash32
	// Hash32 is an alias to types.Hash32.
	Hash32 = types.Hash32
	// Hash20 is an alias to types.Hash20.
	Hash20 = types.Hash20
	// Address is an alias to types.Address.
	Address = types.Address
	// Signature is an alias to types.EdSignature.
	Signature = types.EdSignature

	// Account is an alis to types.Account.
	Account = types.Account
	// Header is an alias to types.TxHeader.
	Header = types.TxHeader
	// Nonce is an alias to types.Nonce.
	Nonce = types.Nonce

	// LayerID is a layer type.
	LayerID = types.LayerID
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/handler.go github.com/spacemeshos/go-spacemesh/vm/core Handler

// Handler provides set of static templates method that are not directly attached to the state.
type Handler interface {
	// Parse header from the payload.
	Parse(*scale.Decoder) (ParseOutput, error)

	// Exec dispatches execution request based on the method selector.
	Exec(Host, *StagedCache, []byte) error

	// New instantiates Template from spawn arguments.
	New([]byte) (Template, error)
	// Load template with stored immutable state.
	Load([]byte) (Template, error)

	// Whether or not this tx is a spawn transaction.
	IsSpawn([]byte) bool
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/template.go github.com/spacemeshos/go-spacemesh/vm/core Template

// Template is a concrete Template type initialized with mutable and immutable state.
type Template interface {
	// MaxSpend decodes MaxSpend value for the transaction. Transaction will fail
	// if it spends more than that.
	MaxSpend(Host, AccountLoader, []byte) (uint64, error)
	// TODO(lane): update to use the VM
	// BaseGas is an intrinsic cost for executing a transaction. If this cost is not covered
	// transaction will be ineffective.
	BaseGas() uint64
	// LoadGas is a cost to load account from disk.
	LoadGas() uint64
	// TODO(lane): update to use the VM
	// ExecGas is a cost to execution a method.
	ExecGas() uint64
	// Verify security of the transaction.
	Verify(Host, []byte, *scale.Decoder) bool
}

// AccountLoader is an interface for loading accounts.
type AccountLoader interface {
	Has(Address) (bool, error)
	Get(Address) (Account, error)
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/updater.go github.com/spacemeshos/go-spacemesh/vm/core AccountUpdater

// AccountUpdater is an interface for updating accounts.
type AccountUpdater interface {
	Update(Account) error
}

// ParseOutput contains all fields that are returned by Parse call.
type ParseOutput struct {
	Nonce    Nonce
	GasPrice uint64
}

// HandlerRegistry stores handlers for templates.
type HandlerRegistry interface {
	Get(Address) Handler
}

// Host API with methods and data that are required by templates.
type Host interface {
	Consume(uint64) error

	Principal() Address
	Nonce() uint64
	TemplateAddress() Address
	MaxGas() uint64
	Handler() Handler
	Template() Template
	Layer() LayerID
	GetGenesisID() Hash20
	Balance() uint64
}

//go:generate scalegen -types Metadata

// Metadata contains generic metadata for all transactions.
type Metadata struct {
	Nonce    Nonce
	GasPrice uint64
}

// Payload contains the opaque, Athena-encoded transaction payload including the method selector
// and method args.
type Payload []byte

func (t *Payload) EncodeScale(enc *scale.Encoder) (total int, err error) {
	{
		n, err := scale.EncodeByteSlice(enc, *t)
		if err != nil {
			return total, err
		}
		total += n
	}

	return total, nil
}

func (t *Payload) DecodeScale(dec *scale.Decoder) (total int, err error) {
	{
		field, n, err := scale.DecodeByteSlice(dec)
		if err != nil {
			return total, err
		}
		total += n
		*t = field
	}

	return total, nil
}
