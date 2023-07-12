package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/cmd/gen"
)

var (
	hrp    = flag.String("hrp", "sm", "network human readable prefix")
	config = flag.String("c", "", "see example json")
)

func must(err error) {
	if err != nil {
		fmt.Println("fatal error: ", err.Error())
		os.Exit(1)
	}
}

func main() {
	flag.Parse()
	types.SetNetworkHRP(*hrp)
	if len(*config) == 0 {
		fmt.Println("please specify config with -c=<path to file>")
		os.Exit(1)
	}

	f, err := os.Open(*config)
	must(err)
	defer f.Close()

	dec := json.NewDecoder(f)
	for {
		var input gen.Input
		if err := dec.Decode(&input); errors.Is(err, io.EOF) {
			break
		} else {
			must(err)
		}
		output := gen.Generate(input)
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(output); err != nil {
			must(err)
		}
	}
}
