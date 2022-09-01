package core

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type (
	// PublicKey is an alias to Hash32.
	PublicKey = types.Hash32
	// Hash32 is an alias to types.Hash32.
	Hash32 = types.Hash32
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
)

// Handler provides set of static templates method that are not directly attached to the state.
type Handler interface {
	// Parse header and arguments from the payload.
	Parse(*Context, uint8, *scale.Decoder) (ParseOutput, scale.Encodable, error)
	// Init instance of the template either by decoding state into Template type or from arguments in case of spawn.
	Init(uint8, any, []byte) (Template, error)
	// Exec dispatches execution request based on the method selector.
	Exec(*Context, uint8, scale.Encodable) error
}

//go:generate mockgen -package=mocks -destination=./mocks/template.go github.com/spacemeshos/go-spacemesh/genvm/core Template

// Template is a concrete Template type initialized with mutable and immutable state.
type Template interface {
	// Template needs to implement scale.Encodable as mutable and immutable state will be stored as a blob of bytes.
	scale.Encodable
	// MaxSpend decodes MaxSpend value for the transaction. Transaction will fail
	// if it spends more than that.
	MaxSpend(uint8, any) (uint64, error)
	// Verify security of the transaction.
	Verify(*Context, []byte, *scale.Decoder) bool
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
	FixedGas uint64
}
