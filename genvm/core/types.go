package core

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type TxType = uint8

const (
	SelfSpawn TxType = iota
	Spawn
	LocalMethodCall
)

type Method = uint16

const MethodSpend Method = 16

type (
	// PublicKey is an alias to Hash32.
	PublicKey = types.Hash32
	// Hash32 is an alias to types.Hash32.
	Hash32 = types.Hash32
	// Hash20 is an alias to types.Hash20.
	Hash20 = types.Hash20
	// Address is an alias to types.Address.
	Address = types.Address
	// Signature is an alias to types.Bytes64.
	Signature = types.Bytes64

	// Account is an alis to types.Account.
	Account = types.Account
	// Header is an alias to types.TxHeader.
	Header = types.TxHeader
	// Nonce is an alias to types.Nonce.
	Nonce = types.Nonce

	// LayerID is a layer type.
	LayerID = types.LayerID
)

//go:generate mockgen -package=mocks -destination=./mocks/handler.go github.com/spacemeshos/go-spacemesh/genvm/core Handler

// Handler provides set of static templates method that are not directly attached to the state.
type Handler interface {
	// Parse header and arguments from the payload.
	Parse(TxType, Method, *scale.Decoder) (ParseOutput, error)
	// Args returns method arguments for the method.
	Args(TxType, Method) scale.Type
	// Exec dispatches execution request based on the method selector.
	Exec(Host, TxType, Method, scale.Encodable) error

	// New instantiates Template from spawn arguments.
	New(any) (Template, error)
	// Load template with stored immutable state.
	Load([]byte) (Template, error)
}

//go:generate mockgen -package=mocks -destination=./mocks/template.go github.com/spacemeshos/go-spacemesh/genvm/core Template

// Template is a concrete Template type initialized with mutable and immutable state.
type Template interface {
	// Template needs to implement scale.Encodable as mutable and immutable state will be stored as a blob of bytes.
	scale.Encodable
	// MaxSpend decodes MaxSpend value for the transaction. Transaction will fail
	// if it spends more than that.
	MaxSpend(Method, any) (uint64, error)
	// Verify security of the transaction.
	Verify(Host, []byte, *scale.Decoder) bool
}

// AccountLoader is an interface for loading accounts.
type AccountLoader interface {
	Get(Address) (Account, error)
}

//go:generate mockgen -package=mocks -destination=./mocks/updater.go github.com/spacemeshos/go-spacemesh/genvm/core AccountUpdater

// AccountUpdater is an interface for updating accounts.
type AccountUpdater interface {
	Update(Account) error
}

// ParseOutput contains all fields that are returned by Parse call.
type ParseOutput struct {
	Nonce    Nonce
	GasPrice uint64
	BaseGas  uint64
	FixedGas uint64
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
}

//go:generate scalegen -types Payload

// Payload is a generic payload for all transactions.
type Payload struct {
	GasPrice uint64
	Nonce    Nonce
}
