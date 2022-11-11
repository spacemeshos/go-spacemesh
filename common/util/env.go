package util

import (
	"os"
	"runtime"
)

const (
	EnvCi        = "CI"
	EnvOsWindows = "windows"
)

// IsWindows checks if we're running on Windows.
func IsWindows() bool {
	return runtime.GOOS == EnvOsWindows
}

// IsCi checks if we're running in CI.
func IsCi() bool {
	return os.Getenv(EnvCi) != ""
}
