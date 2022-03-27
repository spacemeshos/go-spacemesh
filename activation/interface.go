package activation

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type poetValidatorPersistor interface {
	Validate(proof types.PoetProof, poetID []byte, roundID string, signature []byte) error
	StoreProof(proofMessage *types.PoetProofMessage) error
}
