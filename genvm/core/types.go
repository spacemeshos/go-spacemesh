package core

import (
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type (
	PublicKey = scale.PublicKey
	Address   = scale.Address
	Signature = scale.Signature

	Account = types.Account
)

type Handler interface {
	Parse(*Context, uint8, *scale.Decoder) (Header, scale.Encodable, error)
	Init(uint8, any, []byte) (Template, error)
	Exec(*Context, uint8, any) error
}

type Template interface {
	scale.Encodable
	MaxSpend(*Header, uint8, any) error
	Verify(*Context, []byte) bool
}

type AccountLoader interface {
	Get(Address) (Account, error)
}

type AccountUpdater interface {
	Update(Account) error
}
