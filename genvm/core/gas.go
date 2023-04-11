package core

// ComputeIntrinsicGasCost computes intrinsic gas from base gas and storage cost.
func ComputeIntrinsicGasCost(baseGas uint64, tx []byte, storageFactor uint64) uint64 {
	return baseGas + uint64(len(tx))*storageFactor
}

// ComputeGasCost computes total gas cost by adding fixed gas to intrinsic gas cost.
func ComputeGasCost(baseGas, fixedGas uint64, tx []byte, storageFactor uint64) uint64 {
	return ComputeIntrinsicGasCost(baseGas, tx, storageFactor) + fixedGas
}

const (
	// TXDATA is a cost for storing transaction data included into the block. Charged per 8 byte.
	TXDATA = 128
	TX     = 20000
	SPAWN  = 30000
	// STORE is a cost for
	STORE    = 5000
	UPDATE   = 725
	LOAD     = 182
	EDVERIFY = 3000
)
