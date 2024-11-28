package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"os"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/jedib0t/go-pretty/v6/table"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	walletSdk "github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

const MaxPubkeys = 3

func getKeypair() (pub ed25519.PublicKey, priv ed25519.PrivateKey) {
	// generate a random keypair
	pubkey, privkey, err := ed25519.GenerateKey(rand.New(rand.NewSource(rand.Int63())))
	if err != nil {
		log.Fatal("failed to generate ed25519 key")
	}
	return pubkey, privkey
}

func main() {
	t1 := table.NewWriter()
	t1.SetOutputMirror(os.Stdout)
	t1.SetTitle("address test vectors")

	t2 := table.NewWriter()
	t2.SetOutputMirror(os.Stdout)
	t2.SetTitle("transaction test vectors")

	t1Rows := table.Row{
		"pubkey",
		"privkey",
		"principal",
		"network",
		"template",
	}

	// pregenerate keys
	pubkeys := make([]ed25519.PublicKey, MaxPubkeys)
	privkeys := make([]ed25519.PrivateKey, MaxPubkeys)
	for i := range pubkeys {
		pubkeys[i], privkeys[i] = getKeypair()
	}
	t1.AppendHeader(t1Rows)
	t2.AppendHeader(table.Row{
		"method",
		"principal",
		"network",
		"gasPrice",
		"nonce",
		"template",
		"spawnArgs",
		"recipient",
		"amount",
		"tx",
	})
	runNetwork("atest", pubkeys, privkeys, t1, t2)

	t1.Render()
	t2.Render()
}

func runNetwork(hrp string, pubkeys []ed25519.PublicKey, privkeys []ed25519.PrivateKey, t1, t2 table.Writer) {
	libPath, err := host.AthenaLibPath()
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	vmlib, err := athcon.LoadLibrary(libPath)
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	defer vmlib.Close()

	types.SetNetworkHRP(hrp)

	var addrs []types.Address

	// first print the keys and addresses
	for i, pubkey := range pubkeys {
		addr := walletSdk.Address(pubkey)
		addrs = append(addrs, addr)
		t1.AppendRow(table.Row{
			hex.EncodeToString(pubkey),
			hex.EncodeToString(privkeys[i]),
			addr.String(),
			hrp,
			wallet.TemplateAddress.String(),
		})
	}

	spawnSelector, err := athcon.FromString("athexp_spawn")
	if err != nil {
		log.Fatal("failed to generate method selector")
	}
	spendSelector, err := athcon.FromString("athexp_spend")
	if err != nil {
		log.Fatal("failed to generate method selector")
	}

	// next generate and print the transactions
	for i, principal := range addrs {
		tx, err := walletSdk.Spawn(signing.PrivateKey(privkeys[i]), 0)
		if err != nil {
			log.Fatalf("failed to generate spawn transaction: %s", err)
		}
		// first generate a spawn transaction
		t2.AppendRow(table.Row{
			fmt.Sprintf("spawn [%s]", hex.EncodeToString(spawnSelector[:])),
			principal.String(),
			hrp,
			sdk.Defaults().GasPrice,
			0,
			wallet.TemplateAddress.String(),
			hex.EncodeToString(vmlib.EncodeTxSpawn(athcon.Bytes32(pubkeys[i]))),
			"",
			"0",
			hex.EncodeToString(tx),
		})

		// generate some spend txs
		for _, recipient := range addrs[:min(len(addrs), 3)] {
			// generate a random amount and nonce
			amount := rand.Uint64()
			nonce := rand.Uint64()
			tx, err := walletSdk.Spend(signing.PrivateKey(privkeys[i]), recipient, amount, nonce)
			if err != nil {
				log.Fatalf("failed to generate spend transaction: %s", err)
			}
			t2.AppendRow(table.Row{
				fmt.Sprintf("spend [%s]", hex.EncodeToString(spendSelector[:])),
				principal.String(),
				hrp,
				sdk.Defaults().GasPrice,
				nonce,
				wallet.TemplateAddress.String(),
				"",
				recipient.String(),
				amount,
				hex.EncodeToString(tx),
			})
		}
	}
}
