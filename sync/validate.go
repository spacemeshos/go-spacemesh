package sync

import "github.com/spacemeshos/go-spacemesh/types"

type EligibilityValidator interface {
	BlockEligible(block *types.BlockHeader) (bool, error)
}

type ViewValidator interface {
	SyntacticallyValid(block *types.BlockHeader) (bool, error)
}

type TxValidator interface {
	TxValid(tx *types.SerializableTransaction) (bool, error)
}

type AtxValidator interface {
	AtxValid(atx *types.ActivationTx) (bool, error)
}

type blockValidator struct {
	EligibilityValidator
	ViewValidator
	TxValidator
	AtxValidator
}

func NewBlockValidator(bev EligibilityValidator, bsv ViewValidator, txv TxValidator, atxv AtxValidator) BlockValidator {
	return &blockValidator{bev, bsv, txv, atxv}
}
