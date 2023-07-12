package main

import (
	"encoding/hex"
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
	dir     = flag.String("d", ".", "directory with public keys (.pem or .hex)")
	k       = flag.Int("k", 3, "number of required signatures")
	start   = flag.Uint("start", constants.VestStart, "start of the vesting")
	end     = flag.Uint("end", constants.VestEnd, "end of the vesting")
	initial = flag.Uint("initial", 1_000_000, "amount unlocked at the start")
	total   = flag.Uint("total", 10_000_000, "amount unlocked incrementally over end-start period")
	hrp     = flag.String("hrp", "sm", "network human readable prefix")

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
	key := [ed25519.PublicKeySize]byte{}
	n, err := hex.Decode(key[:], data)
	must(err)
	if n != len(key) {
		must(fmt.Errorf("key %s can't be decoded from hex into %d bytes", key, len(key)))
	}
	return key
}

func main() {
	flag.Parse()
	vestingArgs := &multisig.SpawnArguments{
		Required:   uint8(*k),
		PublicKeys: decodeKeys(*dir),
	}
	vestingAddress := core.ComputePrincipal(vesting.TemplateAddress, vestingArgs)
	vaultArgs := &vault.SpawnArguments{
		Owner:               vestingAddress,
		TotalAmount:         uint64(*total),
		InitialUnlockAmount: uint64(*initial),
		VestingStart:        types.LayerID(uint32(*start)),
		VestingEnd:          types.LayerID(uint32(*end)),
	}
	vaultAddress := core.ComputePrincipal(vault.TemplateAddress, vaultArgs)
	types.SetNetworkHRP(*hrp)
	fmt.Printf("vesting: %s\nvault: %s\n", vestingAddress.String(), vaultAddress.String())
	fmt.Println("public keys:")
	for i, key := range vestingArgs.PublicKeys {
		fmt.Printf("%d: %x\n", i, key[:])
	}
}
