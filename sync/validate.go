package sync

import "github.com/spacemeshos/go-spacemesh/types"

type EligibilityValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
}

type TxValidator interface {
	TxValid(tx *types.SerializableTransaction) (bool, error)
}

type AtxValidator interface {
	AtxValid(atx *types.ActivationTx) (bool, error)
}

type blockValidator struct {
	EligibilityValidator
	TxValidator
	AtxValidator
}

func NewBlockValidator(bev EligibilityValidator, txv TxValidator, atxv AtxValidator) BlockValidator {
	return &blockValidator{bev, txv, atxv}
}
