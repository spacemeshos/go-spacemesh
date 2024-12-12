package core

// IntrinsicGas computes intrinsic gas from base gas and storage cost.
func IntrinsicGas(baseGas uint64, txSize int) uint64 {
	return baseGas + TxDataGas(txSize)
}

const (
	// TXDATA is a cost for storing transaction data included into the block. Charged per 8 byte.
	TXDATA uint64 = 128
	// TX is an intrinsic cost for every transaction.
	TX uint64 = 0
	// LOAD is a cost for loading immutable and mutable state from disk.
	LOAD uint64 = 182
	// ACCOUNT_ACCESS is a cost of the account access.
	ACCOUNT_ACCESS uint64 = 2500

	// Hardcoded Athena gas costs
	// TODO(lane): remove hardcoded gas costs.
	ATHENA_GAS_VERIFY = 12_000
)

const (
	PUBLIC_KEY_SIZE      = 32
	ACCOUNT_HEADER_SIZE  = 36 // includes balance (8), nonce (8) and template address (24)
	ACCOUNT_BALANCE_SIZE = 8
)

// SizeGas computes total gas cost for a value of the specific size.
// Gas is charged for every 8 bytes, rounded up.
func SizeGas(gas uint64, size int) uint64 {
	quo := size / 8
	rem := size % 8
	rst := uint64(quo) * gas
	if rem != 0 {
		rst += gas
	}
	return rst
}

func TxDataGas(size int) uint64 {
	return SizeGas(TXDATA, size)
}

func MaxGas(inputSize int) uint64 {
	return 10_000 + uint64(inputSize)*150
}
