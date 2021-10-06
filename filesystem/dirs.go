// Package filesystem provides functionality for interacting with directories and files in a cross-platform manner.
package filesystem

import (
	"fmt"
	"os"
	"os/user"
	"path"
	"strings"
)

// Using a function pointer to get the current user so we can more easily mock in tests
var currentUser = user.Current

// Directory and paths funcs

// OwnerReadWriteExec is a standard owner read / write / exec file permission.
const OwnerReadWriteExec = 0o700

// OwnerReadWrite is a standard owner read / write file permission.
const OwnerReadWrite = 0o600

// PathExists returns true iff file exists in local store and is accessible.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil
}

// GetUserHomeDirectory returns the current user's home directory if one is set by the system.
func GetUserHomeDirectory() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := currentUser(); err == nil {
		return usr.HomeDir
	}
	return ""
}

// GetCanonicalPath returns an os-specific full path following these rules:
// - replace ~ with user's home dir path
// - expand any ${vars} or $vars
// - resolve relative paths /.../
// p: source path name
func GetCanonicalPath(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~\\") {
		if home := GetUserHomeDirectory(); home != "" {
			p = home + p[1:]
		}
	}
	return path.Clean(os.ExpandEnv(p))
}

// GetFullDirectoryPath gets the OS specific full path for a named directory.
// The directory is created if it doesn't exist.
func GetFullDirectoryPath(name string) (string, error) {
	aPath := GetCanonicalPath(name)

	// create dir if it doesn't exist
	if err := os.MkdirAll(aPath, OwnerReadWriteExec); err != nil {
		return aPath, fmt.Errorf("os.MkdirAll: %w", err)
	}

	return aPath, nil
}

// ExistOrCreate creates the given path if it does not exist.
func ExistOrCreate(path string) (err error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, OwnerReadWriteExec); err != nil {
			return fmt.Errorf("os.MkdirAll: %w", err)
		}
	}
	return nil
}
