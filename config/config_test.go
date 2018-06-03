package config

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	err := LoadConfig("./config.toml")
	assert.Nil(t, err)
}
