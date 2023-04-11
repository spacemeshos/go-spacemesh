package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
)

var (
	hrp = flag.String("hrp", "smtest", "network human readable prefix")
)

func main() {
	flag.Usage = func() {
		fmt.Println("usage: addressgen <public key> -hrp=smtest")
	}
	flag.Parse()
	public := flag.Arg(0)
	if len(public) == 0 {
		fmt.Printf("second argument must be a hexary encoded public key: %s, %v", public, flag.Args())
		os.Exit(1)
	}
	bytes, err := hex.DecodeString(public)
	if err != nil {
		fmt.Println("not a hex", err)
		os.Exit(1)
	}
	args := wallet.SpawnArguments{}
	if n := copy(args.PublicKey[:], bytes); n != 32 {
		fmt.Println("decoded key must be 32 bytes")
		os.Exit(1)
	}
	types.DefaultAddressConfig().NetworkHRP = *hrp
	coinbase := core.ComputePrincipal(wallet.TemplateAddress, &args)
	fmt.Printf("wallet: %s\npublic key: %s\ncoinbase: %s\n",
		wallet.TemplateAddress.String(),
		args.PublicKey.String(),
		coinbase.String(),
	)
}
