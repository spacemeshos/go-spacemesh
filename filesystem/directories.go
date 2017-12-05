package filesystem

import (
	"github.com/UnrulyOS/go-unruly/app/config"
	"os"
	"os/user"
	"path"
	"strings"
	"testing"
)

// Directory and paths helpers

const OwnerReadWriteExec = 0700


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
	err := os.MkdirAll(path, OwnerReadWriteExec)

	return path, err
}

// get full os-specific path to the unruly top-level data directory
func GetUnrulyDataDirectoryPath() (string, error) {

	return GetFullDirectoryPath(config.ConfigValues.DataFilePath)
}

// Delete all subfolders and files in the unruly root data folder
func DeleteUnrulyDataFolders(t *testing.T) {

	path, err := GetUnrulyDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get unruly data dir: %v", err)
	}

	// remove
	err = os.RemoveAll(path)
	if err != nil {
		t.Fatalf("Failed to delete unruly data dir: %v", err)
	}

	// create the dir again
	GetUnrulyDataDirectoryPath()
}