package config

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"

	"github.com/spacemeshos/go-spacemesh/log"
)

// GenesisAccount is the json representation of an account
type GenesisAccount struct {
	Balance *big.Int `json:"balance" gencodec:"required"`
	Nonce   uint64   `json:"nonce"`
}

// GenesisConfig defines accounts that will exist in state at genesis
type GenesisConfig struct {
	InitialAccounts map[string]GenesisAccount
}

// SaveGenesisConfig stores account data
func SaveGenesisConfig(path string, config GenesisConfig) error {
	w, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	defer w.Close()
	if err := enc.Encode(&config); err != nil {
		return err
	}
	return nil
}

// LoadGenesisConfig loads config from file if exists
func LoadGenesisConfig(path string) (*GenesisConfig, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		log.Warning("genesis config not loaded since file does not exist. file=%v", path)
		return nil, err
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %v", err)
	}
	defer r.Close()

	dec := json.NewDecoder(r)
	cfg := &GenesisConfig{}
	err = dec.Decode(cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// Account1Pub is the public key for testing
const Account1Pub = "0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account1Private is the private key for test account
const Account1Private = "0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account2Pub is the public key for second test account
const Account2Pub = "0x22a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// Account2Private is the private key for secode test account
const Account2Private = "0x9d411020d46d3f4e1214f7b51052219737669f461ac9c9ac6ac49753926d0af222a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

const (
	tap1 = "c007a95dc5b50c3ebbb8ff6bc4eabd379d646beb6e065934cf7e9de59f8d43e7"
	tap2 = "9707fd32f9e96a72c4fa1b0f18cd748bd82496c4341aad3e66930c855349aa48"
)

// DefaultGenesisConfig is the default configuration for the node
func DefaultGenesisConfig() *GenesisConfig {
	g := GenesisConfig{}

	// we default to 10^5 SMH per account which is 10^17 smidge
	g.InitialAccounts = map[string]GenesisAccount{
		"0x1": {Balance: big.NewInt(int64(math.Pow10(17))), Nonce: 0},
		tap1:  {Balance: big.NewInt(int64(math.Pow10(17))), Nonce: 0},
		tap2:  {Balance: big.NewInt(int64(math.Pow10(17))), Nonce: 0},
	}
	return &g
	//todo: implement reading from file
}
