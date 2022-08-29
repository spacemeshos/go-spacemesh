package core

// ComputeGasCost computes gas cost from fixed gas plus tx size multiplied by storage cost.
func ComputeGasCost(fixedGas uint64, tx []byte, storageFactor uint64) uint64 {
	return fixedGas + uint64(len(tx))*storageFactor
}
