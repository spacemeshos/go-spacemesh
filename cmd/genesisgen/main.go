package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	extra       = flag.String("extra", "", "genesis extra data. valid size is between [1, 255]")
	genesisTime = flag.String("time", "", "genesis time. format "+time.RFC3339)
	n           = flag.Int("n", 10, "number of keys")
)

type output struct {
	N          int    `json:"n"`
	Key        string `json:"private"`
	ID         string `json:"id"`
	Commitment string `json:"commitment"`
}

func main() {
	flag.Parse()
	genesis, err := time.Parse(*genesisTime, time.RFC3339)
	if err != nil {
		fmt.Printf("invalid genesis time: %s\n", err)
		os.Exit(1)
	}

	conf := config.GenesisConfig{GenesisTime: config.Genesis(genesis), ExtraData: *extra}
	if err := conf.Validate(); err != nil {
		fmt.Printf("invalid config values: %s\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	for i := 0; i < *n; i++ {
		pkey, err := signing.NewEdSigner()
		if err != nil {
			fmt.Printf("invalid key: %s\n", err)
			os.Exit(1)
		}
		key := pkey.PrivateKey()
		id := types.BytesToNodeID(signing.Public(signing.PrivateKey(key)))
		if err := encoder.Encode(output{
			N:          i,
			Key:        hex.EncodeToString(key),
			ID:         hex.EncodeToString(id[:]),
			Commitment: hex.EncodeToString(conf.GoldenATX().Bytes()),
		}); err != nil {
			fmt.Printf("failed to encode output %s\n", err)
			os.Exit(1)
		}
	}
}
