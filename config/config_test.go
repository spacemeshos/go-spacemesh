package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

func TestLoadConfig(t *testing.T) {
	// todo: test more
	vip := viper.New()
	err := LoadConfig(".asdasda", vip)
	// verify that after attempting to load a non-existent file, an attempt is made to load the default config
	assert.EqualError(t, err, "failed to read config file open ./config.toml: no such file or directory")
}

func TestConfig_DataDir(t *testing.T) {
	sep := string(filepath.Separator)

	config := DefaultTestConfig()
	config.DataDirParent = "~" + sep + "space-a-mesh"
	expectedDataDir := filesystem.GetUserHomeDirectory() + sep + "space-a-mesh"
	assert.Equal(t, expectedDataDir, config.DataDir())

	config.DataDirParent = "~" + sep + "space-a-mesh" + sep // trailing slash should be ignored
	assert.Equal(t, expectedDataDir, config.DataDir())
}
