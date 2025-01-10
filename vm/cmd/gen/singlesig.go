package main

import (
	"log"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	walletSdk "github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

type singleSig struct {
	signer *signing.EdSigner
}

func (s *singleSig) Spawn(opts ...sdk.Opt) *core.Tx {
	tx, err := walletSdk.SpawnTx(signing.Public(s.signer.PrivateKey()), 0, opts...)
	if err != nil {
		log.Fatalf("failed to generate spawn transaction: %s", err)
	}
	return tx
}

func (s *singleSig) Spend(recipient types.Address, amount, nonce uint64, opts ...sdk.Opt) *core.Tx {
	tx, err := walletSdk.SpendTx(signing.Public(s.signer.PrivateKey()), recipient, amount, nonce)
	if err != nil {
		log.Fatalf("failed to generate spend transaction: %s", err)
	}
	return tx
}

func (s *singleSig) Deploy(nonce uint64, blob []byte) *core.Tx {
	tx, err := walletSdk.DeployTx(signing.Public(s.signer.PrivateKey()), nonce, blob)
	if err != nil {
		log.Fatalf("failed to generate deploy transaction: %s", err)
	}
	return tx
}

func (s *singleSig) Signed(tx *core.Tx, genesisID types.Hash20) []byte {
	signedTx, err := core.SignedTx(tx, genesisID, s.signer.PrivateKey())
	if err != nil {
		log.Fatalf("signing TX: %s", err)
	}
	return signedTx
}

func (*singleSig) TemplateAddress() types.Address {
	return wallet.TemplateAddress
}
