package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/cmd/gen"
)

var (
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

func main() {
	flag.Parse()
	types.SetNetworkHRP(*hrp)
	output := gen.Generate(gen.Input{
		Keys:     hexkeys,
		Required: *k,
		Total:    *total,
	})
	if *debug {
		fmt.Printf("vesting address: %s.\nparameters: %s", output.Debug.VestingAddress, output.Debug.VestingArgs.String())
		fmt.Println("---")
		fmt.Printf("vault address: %s.\nparameters: %s\n", output.Debug.VaultAddress, output.Debug.VaultArgs.String())
	} else {
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(output); err != nil {
			must(err)
		}
	}
}
