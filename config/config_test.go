package config

import (
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// todo: test more
	vip := viper.New()
	err := LoadConfig(".asdasda", vip)
	assert.Error(t, err)
}
