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
	// Parse header and arguments from the payload.
	Parse(*scale.Decoder) (ParseOutput, error)
	// TODO(lane): update to use the VM
	// Args returns method arguments for the method.
	Args() scale.Type

	// TODO(lane): update to use the VM
	// Exec dispatches execution request based on the method selector.
	Exec(Host, scale.Encodable) error

	// New instantiates Template from spawn arguments.
	New(any) (Template, error)
	// Load template with stored immutable state.
	Load([]byte) (Template, error)
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/template.go github.com/spacemeshos/go-spacemesh/vm/core Template

// Template is a concrete Template type initialized with mutable and immutable state.
type Template interface {
	// Template needs to implement scale.Encodable as mutable and immutable state will be stored as a blob of bytes.
	scale.Encodable
	// MaxSpend decodes MaxSpend value for the transaction. Transaction will fail
	// if it spends more than that.
	MaxSpend(any) (uint64, error)
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
	Spawn(scale.Encodable) error
	Transfer(Address, uint64) error
	Relay(expectedTemplate, address Address, call func(Host) error) error

	Principal() Address
	Handler() Handler
	Template() Template
	Layer() LayerID
	GetGenesisID() Hash20
	Balance() uint64
}

//go:generate scalegen -types Payload

// Payload is a generic payload for all transactions.
type Payload struct {
	Nonce    Nonce
	GasPrice uint64
}