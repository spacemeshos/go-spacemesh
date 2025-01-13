package main

import (
	"encoding/hex"
	"fmt"
	"log"
	oldRand "math/rand"
	"math/rand/v2"
	"os"
	"strings"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
)

func main() {
	t1 := table.NewWriter()
	t1.SetOutputMirror(os.Stdout)
	t1.SetTitle("address test vectors")
	t1.AppendHeader(table.Row{
		"template",
		"pubkey(s)",
		"privkey(s)",
		"principal",
		"notes",
		"template",
	})

	t2 := table.NewWriter()
	t2.SetOutputMirror(os.Stdout)
	t2.SetTitle("transaction test vectors")
	t2.AppendHeader(table.Row{
		"method",
		"principal",
		"gasPrice",
		"nonce",
		"template",
		"arguments",
		"recipient",
		"amount",
		"tx",
	})

	types.SetNetworkHRP("atest")
	runNetwork(types.Hash20{}, t1, t2)

	t1.Render()
	t2.Render()
}

type Template interface {
	Spawn(opts ...sdk.Opt) *core.Tx
	Spend(recipient types.Address, amount, nonce uint64, opts ...sdk.Opt) *core.Tx
	Deploy(nonce uint64, blob []byte) *core.Tx

	Signed(tx *core.Tx, genesisID types.Hash20) []byte
	TemplateAddress() types.Address
}

func runNetwork(genesisID types.Hash20, t1, t2 table.Writer) {
	var signers []*signing.EdSigner
	for i := range int64(3) {
		signer, err := signing.NewEdSigner(signing.WithKeyFromRand(oldRand.New(oldRand.NewSource(i))))
		if err != nil {
			log.Fatalf("failed to generate ed25519 key: %v", err)
		}
		signers = append(signers, signer)
	}

	rng := rand.New(&rand.PCG{})
	libPath, err := host.AthenaLibPath()
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	vmlib, err := athcon.LoadLibrary(libPath)
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	defer vmlib.Close()

	spawnSelector, err := athcon.FromString("athexp_spawn")
	if err != nil {
		log.Fatal("failed to generate method selector")
	}
	spendSelector, err := athcon.FromString("athexp_spend")
	if err != nil {
		log.Fatal("failed to generate method selector")
	}

	var contracts []Template
	for _, signer := range signers {
		w := &singleSig{signer}
		contracts = append(contracts, w)

		t1.AppendRow(table.Row{
			"singlesig",
			hex.EncodeToString(signer.PublicKey().Bytes()),
			hex.EncodeToString(signer.PrivateKey()),
			w.Spawn().Principal.String(),
			"",
			w.TemplateAddress().String(),
		})
	}

	w := newMultiSig(2, signers)
	contracts = append(contracts, w)
	var (
		pubkeys  []string
		privkeys []string
	)
	for _, signer := range signers {
		pubkeys = append(pubkeys, hex.EncodeToString(signer.PublicKey().Bytes()))
		privkeys = append(privkeys, hex.EncodeToString(signer.PrivateKey()))
	}
	t1.AppendRow(table.Row{
		"multisig",
		strings.Join(pubkeys, ","),
		strings.Join(privkeys, ","),
		w.Spawn().Principal.String(),
		"requires 2 keys",
		w.TemplateAddress().String(),
	})

	// next generate and print the transactions
	for _, template := range contracts {
		// first generate a spawn transaction
		tx := template.Spawn()
		signedTx := template.Signed(tx, genesisID)
		t2.AppendRow(table.Row{
			fmt.Sprintf("spawn  [%s]", hex.EncodeToString(spawnSelector[:])),
			tx.Principal.String(),
			tx.Metadata.GasPrice,
			tx.Metadata.Nonce,
			template.TemplateAddress().String(),
			hex.EncodeToString(tx.Payload),
			"-",
			"-",
			hex.EncodeToString(signedTx),
		})

		// generate a deploy transaction
		deploySelector, err := athcon.FromString("athexp_deploy")
		if err != nil {
			log.Fatal("failed to generate deploy method selector")
		}
		nonce := rng.Uint64N(1000)
		tx = template.Deploy(nonce, []byte("some code"))
		signedTx = template.Signed(tx, genesisID)
		t2.AppendRow(table.Row{
			fmt.Sprintf("deploy [%s]", hex.EncodeToString(deploySelector[:])),
			tx.Principal.String(),
			sdk.Defaults().GasPrice,
			tx.Metadata.Nonce,
			template.TemplateAddress().String(),
			hex.EncodeToString(tx.Payload),
			"-",
			"-",
			hex.EncodeToString(signedTx),
		})

		// generate some spend txs
		for range 2 {
			amount := rng.Uint64N(50000)
			nonce := rng.Uint64N(1000)
			var recipient types.Address
			for i := range recipient {
				recipient[i] = byte(rng.Uint32())
			}
			tx := template.Spend(recipient, amount, nonce)
			signedTx := template.Signed(tx, genesisID)
			t2.AppendRow(table.Row{
				fmt.Sprintf("spend  [%s]", hex.EncodeToString(spendSelector[:])),
				tx.Principal.String(),
				sdk.Defaults().GasPrice,
				nonce,
				template.TemplateAddress().String(),
				hex.EncodeToString(tx.Payload),
				recipient.String(),
				amount,
				hex.EncodeToString(signedTx),
			})
		}
	}
}
