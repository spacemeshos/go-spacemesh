package config

import (
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/big"
	"os"
)

type GenesisAccount struct {
	Balance *big.Int `json:"balance" gencodec:"required"`
	Nonce   uint64   `json:"nonce"`
}

type GenesisConfig struct {
	InitialAccounts map[string]GenesisAccount
}

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

func LoadGenesisConfig(path string) (*GenesisConfig, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		log.Warning("genesis config not lodad since file does not exist. file=%v", path)
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

const Account1Pub = "0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"
const Account1Private = "0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

func DefaultGenesisConfig() *GenesisConfig {
	g := GenesisConfig{}

	g.InitialAccounts = map[string]GenesisAccount{
		"0x1": {Balance: big.NewInt(10000), Nonce: 0},
		"0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c": {Balance: big.NewInt(10000), Nonce: 0},
	}
	return &g
	//todo: implement reading from file
}
