package filesystem

import (
	"os"
	"testing"
)

// Deletes all sub directories and files in the Spacemesh root data folder
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
