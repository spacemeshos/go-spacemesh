package activation

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type poetValidatorPersistor interface {
	HasProof([]byte) bool
	Validate(types.PoetProof, []byte, string, []byte) error
	StoreProof([]byte, *types.PoetProofMessage) error
}
