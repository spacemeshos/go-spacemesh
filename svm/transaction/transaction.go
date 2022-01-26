package transaction

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// GenerateSpawnTransaction generates a spawn transaction.
func GenerateSpawnTransaction(signer *signing.EdSigner, target types.Address) *types.Transaction {
	inner := types.InnerTransaction{
		Recipient: target,
	}

	buf, err := types.InterfaceToBytes(&inner)
	if err != nil {
		return nil
	}

	sst := &types.Transaction{
		InnerTransaction: inner,
		Signature:        [64]byte{},
	}

	copy(sst.Signature[:], signer.Sign(buf))
	sst.SetOrigin(types.GenerateAddress(signer.PublicKey().Bytes()))

	return sst
}

// GenerateCallTransaction generates a call transaction.
func GenerateCallTransaction(signer *signing.EdSigner, rec types.Address, nonce, amount, gas, fee uint64) (*types.Transaction, error) {
	inner := types.InnerTransaction{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		Fee:          fee,
	}

	buf, err := types.InterfaceToBytes(&inner)
	if err != nil {
		return nil, fmt.Errorf("serialize: %w", err)
	}

	sst := &types.Transaction{
		InnerTransaction: inner,
		Signature:        [64]byte{},
	}

	copy(sst.Signature[:], signer.Sign(buf))
	sst.SetOrigin(types.GenerateAddress(signer.PublicKey().Bytes()))

	return sst, nil
}
