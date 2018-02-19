package filesystem

import (
	"os"
	"os/user"
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

// TestUsers returns a map of users for testing
func TestUsers() map[string]*user.User {
	return map[string]*user.User{
		"alice": {
			Uid:      "100",
			Gid:      "500",
			Username: "alice",
			Name:     "Alice Smith",
			HomeDir:  "/home/alice",
		},
		"bob": {
			Uid:      "200",
			Gid:      "500",
			Username: "bob",
			Name:     "Bob Jones",
			HomeDir:  "/home/bob",
		},
		"michael": {
			Uid:      "300",
			Gid:      "500",
			Username: "michael",
			Name:     "Michael Smith",
			HomeDir:  "",
		},
	}
}

// SetupTestHooks sets current user to mock user to test
func SetupTestHooks(users map[string]*user.User) {
	currentUser = func() (*user.User, error) {
		return users["michael"], nil
	}
}

// TearDownTestHooks sets current user back
func TearDownTestHooks() {
	currentUser = user.Current
}
