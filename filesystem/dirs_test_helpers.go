package filesystem

import (
	"io/ioutil"
	"os"
	"testing"
)

// DeleteSpacemeshDataFolders deletes all sub directories and files in the Spacemesh root data folder.
func DeleteSpacemeshDataFolders(t *testing.T) {

	aPath, err := GetSpacemeshDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get spacemesh data dir: %s", err)
	}

	// remove
	err = os.RemoveAll(aPath)
	if err != nil {
		t.Fatalf("Failed to delete spacemesh data dir: %s", err)
	}
}

// CreateTmpDir creates a temp directory.
func CreateTmpDir(prefix string) (string, error) {
	tempDir, err := ioutil.TempDir("/tmp", prefix)
	if err != nil {
		return tempDir, err
	}
	return tempDir, nil
}

// RemoveTmpDir removes a temp directory.
func RemoveTmpDir(tempDir string) error {
	err := os.Remove(tempDir)
	if err != nil {
		return err
	}
	return nil
}
