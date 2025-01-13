package core

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const TxSizeLimit = 1024 * 1024

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

//go:generate mockgen -typed -package=mocks -destination=./mocks/loader.go github.com/spacemeshos/go-spacemesh/vm/core AccountLoader

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

//go:generate mockgen -typed -package=mocks -destination=./mocks/host.go github.com/spacemeshos/go-spacemesh/vm/core Host

// Host API with methods and data that are required by templates.
type Host interface {
	Clone() Host

	Consume(uint64) error
	Transfer(Address, uint64) error

	Principal() Address
	Nonce() uint64
	Payload() []byte
	TemplateAddress() Address
	Template() []byte
	MaxGas() uint64
	SpendGas(uint64)
	GasSpent() uint64
	Deploy([]byte) (Address, error)
	Spawn(Address, []byte) (Address, error)
	SetStorage(Address, [32]byte, [32]byte) (StorageStatus, error)
	Has(Address) (bool, error)
	Get(Address) (*Account, error)
	Layer() LayerID
	Balance() uint64
	IsSpawn() bool
}

// static context is fixed for the lifetime of one transaction.
type StaticContext struct {
	Principal   types.Address
	Destination types.Address
}

// dynamic context may change with each call frame.
type DynamicContext struct {
	Template types.Address
	Callee   types.Address
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/vmhost.go github.com/spacemeshos/go-spacemesh/vm/core VMHost

// VM Host API.
type VMHost interface {
	Execute(types.LayerID, int64, types.Address, types.Address, []byte, uint64, []byte) ([]byte, int64, error)
}

//go:generate scalegen -types Metadata,Tx

// Metadata contains generic metadata for all transactions.
type Metadata struct {
	Nonce    Nonce
	GasPrice uint64
}

type Tx struct {
	Version   uint8
	Principal types.Address
	// Template is needed for a spawning the prinipal account.
	// It is only allowed to be set when principal is not spawned yet.
	Template *types.Address
	Metadata Metadata
	Payload  []byte `scale:"max=1048576"`
}
