package filesystem

import (
	"github.com/UnrulyOS/go-unruly/app/config"
	"os"
	"os/user"
	"path"
	"strings"
)

// Directory and paths helpers

// Returns the user home directory if one is set
func GetUserHomeDirectory() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if usr, err := user.Current(); err == nil {
		return usr.HomeDir
	}
	return ""
}

// Returns an os-specific full path:
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

// Gets the OS specific full path for a named directory.
// The directory is created if it doesn't exist
func GetFullDirectoryPath(name string) (string, error) {

	path := GetCanonicalPath(name)

	// create dir if it doesn't exist
	err := os.MkdirAll(path, 0700)

	return path, err
}

// get full os-specific path to the unruly top-level data directory
func GetUnrulyDataDirectoryPath() (string, error) {
	return GetFullDirectoryPath(config.ConfigValues.DataFilePath)
}
