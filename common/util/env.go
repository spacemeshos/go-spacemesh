package util

import "os"

const (
	EnvCi        = "CI"
	EnvOs        = "GOOS"
	EnvOsWindows = "windows"
)

// IsWindows checks if we're running on Windows.
func IsWindows() bool {
	return os.Getenv(EnvOs) == EnvOsWindows
}

// IsCi checks if we're running in CI.
func IsCi() bool {
	return os.Getenv(EnvCi) != ""
}
