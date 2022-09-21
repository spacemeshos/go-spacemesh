package config

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Account1Private is the private key for test account.
const Account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"

// Account2Private is the private key for second test account.
const Account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b49feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"

// DefaultGenesisAccounts is the default configuration for the node.
func DefaultGenesisAccounts() map[string]uint64 {
	return generateGenesisAccounts()
}

// DefaultTestGenesisAccounts is the default test configuration for the node.
func DefaultTestGenesisAccounts() map[string]uint64 {
	return generateGenesisAccounts()
}

func generateGenesisAccounts() map[string]uint64 {
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account1Private))
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account2Private))
	if err != nil {
		panic("could not build ed signer")
	}

	// we default to 10^8 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	return map[string]uint64{
		types.GenerateAddress(acc1Signer.PublicKey().Bytes()).String(): 100000000000000000,
		types.GenerateAddress(acc2Signer.PublicKey().Bytes()).String(): 100000000000000000,
	}
}
