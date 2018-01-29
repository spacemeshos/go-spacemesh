package filesystem

import (
	"github.com/stretchr/testify/assert"
	"os/user"
	"testing"
)

var RootFolder = "/"

func TestPathExists(t *testing.T) {
	tempDir, err := CreateTmpDir("TestPathExists")
	assert.NoErrorf(t, err, "creating temp dir failed: %s", err)
	if assert.True(t, PathExists(tempDir)) != true {
		t.Errorf("was expecting function to return true (dir: %s)", tempDir)
	}
	assert.NoErrorf(t, RemoveTmpDir(tempDir), "removing dir failed: %s", err)
	if assert.False(t, PathExists(tempDir)) != true {
		t.Errorf("was expecting function to return true (dir: %s)", tempDir)
	}
}

func TestGetFullDirectoryPath(t *testing.T) {
	tempDir, err := CreateTmpDir("TestGetFullDirectoryPath")
	assert.NoErrorf(t, err, "creating temp dir failed: %s", err)
	aPath, err := GetFullDirectoryPath(tempDir)
	assert.Equal(t, tempDir, aPath)
	assert.Nil(t, err)
	assert.NoErrorf(t, RemoveTmpDir(tempDir), "removing dir failed: %s", err)
}

func TestGetUserHomeDirectory(t *testing.T) {
	usr, err := user.Current()
	assert.NoErrorf(t, err, "getting current user failed: %s", err)
	assert.Equal(t, usr.HomeDir, GetUserHomeDirectory())
}

func TestGetCanonicalPath(t *testing.T) {
	t.Parallel()
	usr, err := user.Current()
	assert.NoErrorf(t, err, "getting current user failed: %s", err)
	//t.Error(usr.HomeDir)
	testCases := []struct {
		path     string
		expected string
	}{
		{"", "."},
		{".", "."},
		{"spacemesh", "spacemesh"},
		{"spacemesh/app/config", "spacemesh/app/config"},
		{"spacemesh/../test", "test"},
		{"spacemesh/../..", ".."},
		{"spacemesh/.././../test", ".." + RootFolder + "test"},
		{"a/b/../c/d/..", "a/c"},
		{"~/spacemesh/test/../config/app/..", usr.HomeDir + RootFolder + "spacemesh/config"},
		{RootFolder + "spacemesh", RootFolder + "spacemesh"},
		{RootFolder + "spacemesh/app/config", RootFolder + "spacemesh/app/config"},
		{RootFolder + "spacemesh/../test", RootFolder + "test"},
	}

	for _, testCase := range testCases {
		actual := GetCanonicalPath(testCase.path)
		assert.Equal(t, testCase.expected, actual, "For path %s", testCase.path)
	}
}
