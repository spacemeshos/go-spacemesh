package config

import (
<<<<<<< HEAD
	"errors"
=======
	"encoding/json"
	"fmt"
	"io/ioutil"
>>>>>>> develop
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

<<<<<<< HEAD
const (
	DefaultGenesisConfigFileName = "genesis.conf"
	defaultGenesisExtraData      = "mainnet"

	account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"
	account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b49feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"
)

type GenesisConfig struct {
	Accounts    map[string]uint64 `mapstructure:"accounts"`
	GenesisTime string            `mapstructure:"genesis-time"`
	ExtraData   string            `mapstructure:"genesis-extradata"`
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    GenerateDefaultGenesisAccounts(),
		GenesisTime: time.Now().Format(time.RFC3339),
		ExtraData:   defaultGenesisExtraData,
	}
}

func GenerateDefaultGenesisAccounts() map[string]uint64 {
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(account1Private))
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(account2Private))
	if err != nil {
		panic("could not build ed signer")
	}

	// we default to 10^8 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	return map[string]uint64{
		types.GenerateAddress(acc1Signer.PublicKey().Bytes()).String(): 100000000000000000,
		types.GenerateAddress(acc2Signer.PublicKey().Bytes()).String(): 100000000000000000,
	}
}

// ToAccountList creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccountList() []types.Account {
=======
// GenesisConfig contains immutable parameters for the protocol.
type GenesisConfig struct {
	GenesisTime string            `mapstructure:"genesis-time"`
	ExtraData   string            `mapstructure:"extra-data"`
	Accounts    map[string]uint64 `mapstructure:"accounts"`
}

// GenesisID computes genesis id from GenesisTime and ExtraData.
func (g *GenesisConfig) GenesisID() types.Hash20 {
	hh := hash.New()
	hh.Write([]byte(g.GenesisTime))
	hh.Write([]byte(g.ExtraData))
	return types.BytesToHash(hh.Sum(nil)).ToHash20()
}

// Validate GenesisConfig.
func (g *GenesisConfig) Validate() error {
	if len(g.ExtraData) > 255 {
		return fmt.Errorf("extra-data is longer than 255 symbols: %s", g.ExtraData)
	}
	_, err := time.Parse(time.RFC3339, g.GenesisTime)
	if err != nil {
		return fmt.Errorf("can't parse genesis time %s using time.RFC3339(%s) %w",
			g.GenesisTime, time.RFC3339, err)
	}
	return nil
}

// Diff returns difference between two configs.
func (g *GenesisConfig) Diff(other *GenesisConfig) string {
	return cmp.Diff(g, other)
}

// LoadFromFile loads config from file.
func (g *GenesisConfig) LoadFromFile(filename string) error {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, g)
}

// WriteToFile writes config content to file.
func (g *GenesisConfig) WriteToFile(filename string) error {
	buf, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, buf, 0o644)
}

// ToAccounts creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccounts() []types.Account {
>>>>>>> develop
	var rst []types.Account
	for addr, balance := range g.Accounts {
		genesisAddr, err := types.StringToAddress(addr)
		if err != nil {
			log.Panic("could not create address from genesis config `%s`: %s", addr, err.Error())
		}
		rst = append(rst, types.Account{
			Address: genesisAddr,
			Balance: balance,
		})
	}
	return rst
}

<<<<<<< HEAD
func (gc *GenesisConfig) Compare(stored *GenesisConfig, path string, lg log.Log) error {
	if diff := cmp.Diff(stored, gc); diff != "" {
		lg.Error("Genesis config %s changed from previous run (-want +got):\n%s", path, diff)
		return errors.New("failed to match config file")
	}
	return nil
}

func CalcGenesisID(genesisExtraData, genesisTime string) types.Hash20 {
	hasher := hash.New()
	hasher.Write([]byte(genesisTime))
	hasher.Write([]byte(genesisExtraData))
	digest := hasher.Sum([]byte{})
	return types.BytesToHash(digest).ToHash20()
}

func (gc *GenesisConfig) GenesisID() uint64 {
	return CalcGenesisID(gc.ExtraData, gc.GenesisTime).Big().Uint64()
=======
// Account1Private is the private key for test account.
const Account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"

// Account2Private is the private key for second test account.
const Account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b49feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"

// DefaultGenesisConfig is the default configuration for the node.
func DefaultGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	return &GenesisConfig{
		ExtraData:   "mainnet",
		GenesisTime: time.Now().Format(time.RFC3339),
		Accounts:    generateGenesisAccounts(),
	}
}

// DefaultTestGenesisConfig is the default test configuration for the node.
func DefaultTestGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	return &GenesisConfig{
		ExtraData:   "testnet",
		GenesisTime: time.Now().Format(time.RFC3339),
		Accounts:    generateGenesisAccounts(),
	}
}

func generateGenesisAccounts() map[string]uint64 {
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account1Private))
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account2Private))
	if err != nil {
		panic("could not build ed signer")
	}

	// we default to 10^8 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	return map[string]uint64{
		types.GenerateAddress(acc1Signer.PublicKey().Bytes()).String(): 100000000000000000,
		types.GenerateAddress(acc2Signer.PublicKey().Bytes()).String(): 100000000000000000,
	}
>>>>>>> develop
}
