package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
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
	home, err := os.UserHomeDir()
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "space-a-mesh"), config.DataDir())

	config.DataDirParent = "~" + sep + "space-a-mesh" + sep // trailing slash should be ignored
	assert.Equal(t, filepath.Join(home, "space-a-mesh"), config.DataDir())
}
