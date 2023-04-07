package main

import (
	"flag"
	"fmt"
	"io/ioutil"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	key   = flag.String("key", "", "ed25519 keyfile")
	extra = flag.String("extra", "", "genesis extra data")
	time  = flag.String("time", "", "genesis time")
)

func main() {
	flag.Parse()

	conf := config.GenesisConfig{GenesisTime: *time, ExtraData: *extra}
	key, err := ioutil.ReadFile(*key)
	if err != nil {
		pkey, err := signing.NewEdSigner()
		if err != nil {
			panic(err)
		}
		key = pkey.PrivateKey()
		fmt.Printf("key is not found. i generated one for you:\n%x\n", key)
	}

	id := types.BytesToNodeID(signing.Public(signing.PrivateKey(key)))
	fmt.Println("---")
	fmt.Println("values below can be passed to post")
	fmt.Printf("id %v\ngolden %x\n", id.String(), conf.GenesisID().ToHash32())
}
