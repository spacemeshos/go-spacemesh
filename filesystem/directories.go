// Package filesystem provides functionality for interacting with directories and files in a cross-platform manner.
package filesystem

import (
	"github.com/spacemeshos/go-spacemesh/app/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// Directory and paths funcs
const OwnerReadWriteExec = 0700
const OwnerReadWrite = 0600

// Return true iff file exists and is accessible
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err != nil
}

// Get the full os-specific path to the spacemesh top-level data directory
func GetSpaceMeshDataDirectoryPath() (string, error) {
	return GetFullDirectoryPath(config.ConfigValues.DataFilePath)
}

// Get the spacemesh temp files dir so we don't have to work with convoluted os specific temp folders
func GetSpaceMeshTempDirectoryPath() (string, error) {

	dataDir, err := GetFullDirectoryPath(config.ConfigValues.DataFilePath)
	if err != nil {
		log.Error("Failed to get data directory: %v", err)
		return "", err
	}

	pathName := filepath.Join(dataDir, "temp")
	return GetFullDirectoryPath(pathName)
}

// Return the os-specific path to the SpaceMesh data folder
// Creates it and all subdirs on demand
func EnsureSpaceMeshDataDirectories() (string, error) {
	dataPath, err := GetSpaceMeshDataDirectoryPath()
	if err != nil {
		log.Error("Can't get or create spacemesh data folder")
		return "", err
	}

	// ensure sub folders exist - create them on demand
	_, err = GetAccountsDataDirectoryPath()
	if err != nil {
		return "", err
	}

	_, err = GetLogsDataDirectoryPath()
	if err != nil {
		return "", err
	}

	return dataPath, nil
}

// Ensure a sub-directory exists
func ensureDataSubDirectory(dirName string) (string, error) {
	dataPath, err := GetSpaceMeshDataDirectoryPath()
	if err != nil {
		log.Error("Failed to ensure data dir: %v", err)
		return "", err
	}

	pathName := filepath.Join(dataPath, dirName)
	aPath, err := GetFullDirectoryPath(pathName)
	if err != nil {
		log.Error("Can't access spacemesh folder: %v", pathName)
		return "", err
	}
	return aPath, nil
}

func GetAccountsDataDirectoryPath() (string, error) {
	aPath, err := ensureDataSubDirectory(config.AccountsDirectoryName)
	if err != nil {
		log.Error("Can't access spacemesh accounts folder. %v", err)
		return "", err
	}
	return aPath, nil
}

func GetLogsDataDirectoryPath() (string, error) {
	aPath, err := ensureDataSubDirectory(config.LogDirectoryName)
	if err != nil {
		log.Error("Can't access spacemesh logs folder. %v", err)
		return "", err
	}
	return aPath, nil
}

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

	aPath := GetCanonicalPath(name)

	// create dir if it doesn't exist
	err := os.MkdirAll(aPath, OwnerReadWriteExec)

	return aPath, err
}

// Delete all subfolders and files in the spacemesh root data folder
func DeleteSpaceMeshDataFolders(t *testing.T) {

	aPath, err := GetSpaceMeshDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get spacemesh data dir: %v", err)
	}

	// remove
	err = os.RemoveAll(aPath)
	if err != nil {
		t.Fatalf("Failed to delete spacemesh data dir: %v", err)
	}

	// create the dir again
	GetSpaceMeshDataDirectoryPath()
}
