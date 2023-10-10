//go:build windows

package activation

import (
	"os/exec"
	"path/filepath"
)

// DefaultPostServiceConfig returns the default config for post service.
func DefaultPostServiceConfig() PostSupervisorConfig {
	return PostSupervisorConfig{
		PostServiceCmd: "C:\\service.exe",
		NodeAddress:    "http://127.0.0.1:9093",
	}
}

// DefaultTestPostServiceConfig returns the default config for post service in tests.
func DefaultTestPostServiceConfig() PostSupervisorConfig {
	path, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		panic(err)
	}

	return PostSupervisorConfig{
		PostServiceCmd: filepath.Join(filepath.Dir(string(path)), "build", "service.exe"),
		NodeAddress:    "http://127.0.0.1:9093",
	}
}
