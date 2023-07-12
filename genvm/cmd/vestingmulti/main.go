package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/cmd/gen"
)

var (
	hrp    = flag.String("hrp", "sm", "network human readable prefix")
	out    = flag.String("out", "json", "json|go")
	config = flag.String("c", "", "see example json")
)

func must(err error) {
	if err != nil {
		fmt.Println("fatal error: ", err.Error())
		os.Exit(1)
	}
}

func parseJson(f io.Reader) []gen.Input {
	dec := json.NewDecoder(f)
	inputs := []gen.Input{}
	for {
		var input gen.Input
		if err := dec.Decode(&input); errors.Is(err, io.EOF) {
			break
		} else {
			must(err)
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func parseCsv(f io.Reader) []gen.Input {
	scan := bufio.NewScanner(f)
	var inputs []gen.Input
	for scan.Scan() {
		var input gen.Input
		columns := strings.Split(scan.Text(), ",")
		total, err := strconv.ParseFloat(columns[0], 64)
		must(err)
		k, err := strconv.ParseUint(columns[1], 10, 8)
		must(err)
		n, err := strconv.ParseInt(columns[2], 10, 8)
		must(err)
		input.Required = int(k)
		input.Total = total
		for i := int64(3); i < 3+n; i++ {
			key := columns[i]
			input.Keys = append(input.Keys, key)
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func parseInputs() []gen.Input {
	f, err := os.Open(*config)
	must(err)
	defer f.Close()

	if strings.HasSuffix(f.Name(), "json") {
		return parseJson(f)
	} else if strings.HasSuffix(f.Name(), "csv") {
		return parseCsv(f)
	}
	fmt.Printf("unknown file extension for file %v. expect json or csv", f.Name())
	os.Exit(1)
	return nil
}

func main() {
	flag.Parse()
	types.SetNetworkHRP(*hrp)
	if len(*config) == 0 {
		fmt.Println("please specify config with -c=<path to file>")
		os.Exit(1)
	}

	inputs := parseInputs()
	var outputs []gen.Output
	for _, input := range inputs {
		outputs = append(outputs, gen.Generate(input))
	}
	switch *out {
	case "json":
		for _, output := range outputs {
			enc := json.NewEncoder(os.Stdout)
			if err := enc.Encode(output); err != nil {
				must(err)
			}
		}
	case "go":
		fmt.Println("func MainnetAccounts() map[string]uint64 {")
		fmt.Println("    return map[string]uint64{")
		for _, output := range outputs {
			fmt.Printf("        \"%s\": %d,\n", output.Address, output.Balance)
		}
		fmt.Println("    }")
		fmt.Println("}")
	}
}
