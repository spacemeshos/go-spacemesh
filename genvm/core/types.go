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

type TemplateAPI interface {
	Parse(*Context, uint8, *scale.Decoder) (Header, scale.Encodable)
	Load(*Context, uint8, any) Template
	Exec(*Context, uint8, any)
}

type Template interface {
	scale.Encodable
	Verify(*Context, []byte) bool
}
