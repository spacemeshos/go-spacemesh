package filesystem

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"os/user"
	"testing"
)

var RootFolder = "/"

func TestPathExists(t *testing.T) {
	tempDir, err := CreateTmpDir("TestPathExists")
	assert.NoErr(t, err, "creating temp dir failed")
	assert.True(t, PathExists(tempDir), "expecting existence of path")
	assert.NoErr(t, RemoveTmpDir(tempDir), "removing dir failed")
	assert.False(t, PathExists(tempDir), "expecting non-existence of path")
}

func TestGetFullDirectoryPath(t *testing.T) {
	tempDir, err := CreateTmpDir("TestGetFullDirectoryPath")
	assert.NoErr(t, err, "creating temp dir failed")
	aPath, err := GetFullDirectoryPath(tempDir)
	assert.Equal(t, tempDir, aPath, "Path is different")
	assert.Nil(t, err)
	assert.NoErr(t, RemoveTmpDir(tempDir), "removing dir failed")
}

func TestGetUserHomeDirectory(t *testing.T) {
	usr, err := user.Current()
	assert.NoErr(t, err, "getting current user failed")
	assert.Equal(t, usr.HomeDir, GetUserHomeDirectory(), "Path is different")
}

func TestGetCanonicalPath(t *testing.T) {
	t.Parallel()
	usr, err := user.Current()
	assert.NoErr(t, err, "getting current user failed")
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
		assert.Equal(t, testCase.expected, actual, "Path is different")
	}
}
