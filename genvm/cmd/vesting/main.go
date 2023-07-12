package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/economics/constants"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
)

var (
	dir   = flag.String("d", ".", "directory with public keys (.pem or .hex). it will be used if keys are not provided with commandline")
	k     = flag.Int("k", 1, "number of required signatures")
	total = flag.Float64("total", 10e9, "total vaulted amount. 1/4 of it is vested at the VestStart (~1 year)")
	hrp   = flag.String("hrp", "sm", "network human readable prefix")
	debug = flag.Bool("debug", false, "output all data. otherwise output only vault and balance in json format")

	hexkeys []string
)

func init() {
	flag.Func("key", "add a public key to multisig encoded in hex (64 characters, without 0x)", func(key string) error {
		hexkeys = append(hexkeys, key)
		return nil
	})
}

func must(err error) {
	if err != nil {
		fmt.Println("fatal error: ", err.Error())
		os.Exit(1)
	}
}

const pemext = ".pem"

func decodeKeys(dir string) []core.PublicKey {
	var keys []core.PublicKey
	if len(hexkeys) > 0 {
		for _, data := range hexkeys {
			keys = append(keys, decodeHexKey([]byte(data)))
		}
		return keys
	}
	files, err := os.ReadDir(dir)
	must(err)
	for _, file := range files {
		fname := filepath.Join(dir, file.Name())
		f, err := os.Open(fname)
		must(err)
		defer f.Close()
		data, err := io.ReadAll(f)
		must(err)
		if filepath.Ext(fname) == pemext {
			block, _ := pem.Decode(data)
			key := [ed25519.PublicKeySize]byte{}
			n := copy(key[:], block.Bytes[len(block.Bytes)-len(key):])
			if n != len(key) {
				must(fmt.Errorf("key in pem file %s is not of the expected format", fname))
			}
			keys = append(keys, key)
		} else {
			keys = append(keys, decodeHexKey(data))
		}
	}
	return keys
}

func decodeHexKey(data []byte) [ed25519.PublicKeySize]byte {
	data = bytes.Trim(data, "\n")
	key := [ed25519.PublicKeySize]byte{}
	n, err := hex.Decode(key[:], data)
	must(err)
	if n != len(key) {
		must(fmt.Errorf("key %s can't be decoded from hex into %d bytes", key, len(key)))
	}
	return key
}

type vaultInfo struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
}

func main() {
	flag.Parse()

	vestingArgs := &multisig.SpawnArguments{
		Required:   uint8(*k),
		PublicKeys: decodeKeys(*dir),
	}
	if int(vestingArgs.Required) > len(vestingArgs.PublicKeys) {
		fmt.Printf("requires more signatures (%d) then public keys (%d) in the wallet\n",
			vestingArgs.Required,
			len(vestingArgs.PublicKeys),
		)
		os.Exit(1)
	}
	vestingAddress := core.ComputePrincipal(vesting.TemplateAddress, vestingArgs)
	vaultArgs := &vault.SpawnArguments{
		Owner:               vestingAddress,
		TotalAmount:         uint64(*total),
		InitialUnlockAmount: uint64(*total) / 4,
		VestingStart:        types.LayerID(constants.VestStart),
		VestingEnd:          types.LayerID(constants.VestEnd),
	}
	vaultAddress := core.ComputePrincipal(vault.TemplateAddress, vaultArgs)
	types.SetNetworkHRP(*hrp)
	if *debug {
		fmt.Printf("vesting address: %s.\nparameters: %s", vestingAddress.String(), vestingArgs)
		fmt.Println("---")
		fmt.Printf("vault address: %s.\nparameters: %s\n", vaultAddress.String(), vaultArgs)
	} else {
		info := vaultInfo{
			Address: vaultAddress.String(),
			Balance: uint64(*total),
		}
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(&info); err != nil {
			must(err)
		}
	}
}
