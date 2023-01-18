package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=weakcoin -destination=./mocks.go -source=./interface.go

type vrfSigner interface {
	Sign(msg []byte) []byte
	PublicKey() *signing.PublicKey
	LittleEndian() bool
}
