package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	draft    bool
	nonce    uint64
	address  string
	keyFiles []string
	keys     = make(map[int]signing.PrivateKey)
)

func getKey(i int) (signing.PrivateKey, error) {
	if k, ok := keys[i]; ok {
		return k, nil
	}
	if len(keyFiles) < i {
		return nil, errors.New("key file was not provided")
	}
	signer, err := signing.NewEdSigner(signing.FromFile(keyFiles[i]))
	if err != nil {
		return nil, fmt.Errorf("reading the key: %s", err)
	}
	keys[i] = signer.PrivateKey()
	return signer.PrivateKey(), nil
}

var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "application to interact with transactions on an Athena network",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&draft, "draft", false, "prepare and print but do not submit the TX")
	rootCmd.PersistentFlags().Uint64Var(&nonce, "nonce", math.MaxUint64, "Nonce for the spawn transaction")
	rootCmd.PersistentFlags().
		StringVar(&address, "address", "localhost:9092", "Address of the node to submit the TX to")
	rootCmd.PersistentFlags().
		StringSliceVarP(&keyFiles, "principal-key", "p", []string{"key.hex"}, "path to file with the principal key")
}
