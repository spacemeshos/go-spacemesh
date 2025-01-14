package main

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	sdkmultisig "github.com/spacemeshos/go-spacemesh/vm/sdk/multisig"
	"github.com/spacemeshos/go-spacemesh/vm/templates/multisig"
)

type multiSigWallet struct {
	required uint8
	pks      []ed25519.PrivateKey
	address  types.Address
}

func newMultiSig(required uint8, signers []*signing.EdSigner) *multiSigWallet {
	var pks []ed25519.PrivateKey
	var pubs []core.PublicKey
	for _, signer := range signers {
		pk := signer.PrivateKey()
		pks = append(pks, pk)
		pubs = append(pubs, core.PublicKey(signing.Public(pk)))
	}
	return &multiSigWallet{
		required: required,
		pks:      pks,
		address:  sdkmultisig.Address(multisig.TemplateAddress, required, pubs),
	}
}

func (*multiSigWallet) TemplateAddress() types.Address {
	return multisig.TemplateAddress
}

func (m *multiSigWallet) Spawn(opts ...sdk.Opt) *core.Tx {
	var pubs []core.PublicKey
	for _, pk := range m.pks {
		pubs = append(pubs, [32]byte(signing.Public(signing.PrivateKey(pk))))
	}
	return sdkmultisig.SpawnTx(m.required, pubs, 0, opts...)
}

func (m *multiSigWallet) Spend(recipient types.Address, amount, nonce uint64, opts ...sdk.Opt) *core.Tx {
	return sdkmultisig.SpendTx(m.address, recipient, amount, nonce, opts...)
}

func (m *multiSigWallet) Deploy(nonce uint64, blob []byte) *core.Tx {
	return sdkmultisig.DeployTx(m.address, nonce, blob)
}

func (m *multiSigWallet) Signed(tx *core.Tx, genesisID types.Hash20) []byte {
	rawTx := codec.MustEncode(tx)
	agg := sdkmultisig.NewSignatureAggregator(rawTx)
	for i := range m.required {
		pk := m.pks[i]
		sig := core.SignRawTx(rawTx, genesisID, pk)
		agg.Add(uint8(i), core.Signature(sig))
	}
	return agg.Raw()
}
