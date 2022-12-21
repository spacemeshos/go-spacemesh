package core

// ComputeIntrinsicGasCost computes intrinsic gas from base gas and storage cost.
func ComputeIntrinsicGasCost(baseGas uint64, tx []byte, storageFactor uint64) uint64 {
	return baseGas + uint64(len(tx))*storageFactor
}

// ComputeGasCost computes total gas cost by adding fixed gas to intrinsic gas cost.
func ComputeGasCost(baseGas, fixedGas uint64, tx []byte, storageFactor uint64) uint64 {
	return ComputeIntrinsicGasCost(baseGas, tx, storageFactor) + fixedGas
}
