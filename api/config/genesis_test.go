package config

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSaveLoadConfig(t *testing.T) {
	cfg := GenesisConfig{}
	cfg.InitialAccounts = map[string]GenesisAccount{
		"0x1": {Balance: 10000, Nonce: 0},
		"0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c": {Balance: 10000, Nonce: 0},
	}

	tempDir, err := ioutil.TempDir("", "genesis")
	if err != nil {
		t.Fatalf("unable to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := tempDir + "/genesis.cfg"

	err = SaveGenesisConfig(filePath, cfg)
	assert.NoError(t, err)

	gs, err := LoadGenesisConfig(filePath)
	assert.NoError(t, err)
	assert.Equal(t, gs, &cfg)
}
