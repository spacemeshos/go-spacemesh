//go:build !windows

package config

import (
	"os/exec"
	"path/filepath"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.BaseConfig = defaultTestConfig()
	conf.P2P = p2p.DefaultConfig()
	conf.API = grpcserver.DefaultTestConfig()

	path, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		panic(err)
	}
	conf.POSTService.PostServiceCmd = filepath.Join(filepath.Dir(string(path)), "build", "service")
	return conf
}
