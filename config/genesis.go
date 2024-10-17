package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type Genesis time.Time

func (g Genesis) Time() time.Time {
	return time.Time(g)
}

func (g Genesis) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.Time())
}

func (g *Genesis) UnmarshalJSON(data []byte) error {
	var t time.Time
	if err := json.Unmarshal(data, &t); err != nil {
		return err
	}
	*g = Genesis(t)
	return nil
}

func (g Genesis) Equal(other Genesis) bool {
	return time.Time(g).Equal(time.Time(other))
}

func (g Genesis) String() string {
	return g.Time().String()
}

// Set implements pflag.Value.Set.
func (g *Genesis) Set(value string) error {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return err
	}
	*g = Genesis(t)
	return nil
}

// Type implements pflag.Value.Type.
func (Genesis) Type() string {
	return "Genesis"
}

// GenesisConfig contains immutable parameters for the protocol.
type GenesisConfig struct {
	GenesisTime Genesis           `mapstructure:"genesis-time"`
	ExtraData   string            `mapstructure:"genesis-extra-data"`
	Accounts    map[string]uint64 `mapstructure:"accounts"`
}

// GenesisID computes genesis id from GenesisTime and ExtraData.
func (g *GenesisConfig) GenesisID() types.Hash20 {
	return g.GoldenATX().ToHash20()
}

func (g *GenesisConfig) GoldenATX() types.Hash32 {
	hh := hash.GetHasher()
	defer hash.PutHasher(hh)
	hh.Write([]byte(strconv.FormatInt(g.GenesisTime.Time().Unix(), 10)))
	hh.Write([]byte(g.ExtraData))
	return types.BytesToHash(hh.Sum(nil))
}

// Validate GenesisConfig.
func (g *GenesisConfig) Validate() error {
	if len(g.ExtraData) == 0 {
		return errors.New("wait until genesis-extra-data is available")
	}
	if len(g.ExtraData) > 255 {
		return fmt.Errorf("extra-data is longer than 255 symbols: %s", g.ExtraData)
	}
	return nil
}

// Diff returns difference between two configs.
func (g *GenesisConfig) Diff(other *GenesisConfig) string {
	return cmp.Diff(g, other)
}

// LoadFromFile loads config from file.
func (g *GenesisConfig) LoadFromFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(g)
}

// WriteToFile writes config content to file.
func (g *GenesisConfig) WriteToFile(filename string) error {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(g)
}

// ToAccounts creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccounts() []types.Account {
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

// Account1Private is the private key for test account.
const Account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b" +
	"90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"

// Account2Private is the private key for second test account.
const Account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b4" +
	"9feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"

// DefaultGenesisConfig is the default configuration for the node.
func DefaultGenesisConfig() GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	return GenesisConfig{
		ExtraData:   "mainnet",
		GenesisTime: Genesis(time.Now()),
		Accounts:    generateGenesisAccounts(),
	}
}

// DefaultTestGenesisConfig is the default test configuration for the node.
func DefaultTestGenesisConfig() GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	return GenesisConfig{
		ExtraData:   "testnet",
		GenesisTime: Genesis(time.Now()),
		Accounts:    generateGenesisAccounts(),
	}
}

func generateGenesisAccounts() map[string]uint64 {
	acc1Signer, err := signing.NewEdSigner(
		signing.WithPrivateKey(util.FromHex(Account1Private)),
	)
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSigner(
		signing.WithPrivateKey(util.FromHex(Account2Private)),
	)
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
