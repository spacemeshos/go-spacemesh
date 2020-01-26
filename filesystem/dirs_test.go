package filesystem

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"os"
	"os/user"
	"testing"

	"github.com/stretchr/testify/assert"
)

var RootFolder = "/"

func TestPathExists(t *testing.T) {
	tempFile := os.TempDir() + "testdir" + uuid.New().String() + "_" + t.Name()
	_, err := os.Create(tempFile)
	require.NoError(t, err, "couldn't create temp path")
	assert.True(t, PathExists(tempFile), "expecting existence of path")
	os.RemoveAll(tempFile)
}

func TestGetFullDirectoryPath(t *testing.T) {
	tempFile := os.TempDir() + "testdir" + uuid.New().String() + "_" + t.Name()
	err := os.MkdirAll(tempFile, os.ModeDir)
	assert.NoError(t, err, "creating temp dir failed")
	aPath, err := GetFullDirectoryPath(tempFile)
	assert.Equal(t, tempFile, aPath, "Path is different")
	assert.NoError(t, err)
	os.RemoveAll(tempFile)
}

func TestGetUserHomeDirectory(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoError(t, err, "getting current user failed")
	testCases := []struct {
		user            *user.User
		home            string
		expectedHomeDir string
	}{
		{users["alice"], users["alice"].HomeDir, "/home/alice"},
		{users["bob"], users["bob"].HomeDir, "/home/bob"},
		{users["michael"], users["michael"].HomeDir, usr.HomeDir},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.home)
		actual := GetUserHomeDirectory()
		assert.Equal(t, testCase.expectedHomeDir, actual, "HOME is different")
	}

	TearDownTestHooks()
	usr, err = currentUser()
	assert.NoError(t, err, "getting current user failed")
	assert.Equal(t, usr.HomeDir, GetUserHomeDirectory(), "HOME is different")
}

func TestGetCanonicalPath(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoError(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		path     string
		expected string
	}{
		{users["alice"], "", "."},
		{users["bob"], ".", "."},
		{users["alice"], "spacemesh", "spacemesh"},
		{users["bob"], "spacemesh/app/config", "spacemesh/app/config"},
		{users["alice"], "spacemesh/../test", "test"},
		{users["bob"], "spacemesh/../..", ".."},
		{users["bob"], "spacemesh/.././../test", ".." + RootFolder + "test"},
		{users["alice"], "a/b/../c/d/..", "a/c"},
		{usr, "~/spacemesh/test/../config/app/..", "~" + usr.HomeDir + RootFolder + "spacemesh/config"},
		{users["bob"], "~/spacemesh/test/../config/app/..", users["bob"].HomeDir + RootFolder + "spacemesh/config"},
		{users["alice"], RootFolder + "spacemesh", RootFolder + "spacemesh"},
		{users["bob"], RootFolder + "spacemesh/app/config", RootFolder + "spacemesh/app/config"},
		{users["alice"], RootFolder + "spacemesh/../test", RootFolder + "test"},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		actual := GetCanonicalPath(testCase.path)
		assert.Equal(t, testCase.expected, actual, "")
	}
	TearDownTestHooks()
}
