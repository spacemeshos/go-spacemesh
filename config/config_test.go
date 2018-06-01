package config

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	err := LoadConfig("./config.toml")
	assert.Nil(t, err)
}
