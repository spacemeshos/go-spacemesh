package filesystem

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"os"
	"os/user"
	"testing"
)

var RootFolder = "/"

func TestPathExists(t *testing.T) {
	tempDir, err := GetSpacemeshTempDirectoryPath()
	assert.NoErr(t, err, "creating temp dir failed")
	assert.True(t, PathExists(tempDir), "expecting existence of path")
	assert.NoErr(t, DeleteAllTempFiles(), "removing dir failed")
}

func TestGetFullDirectoryPath(t *testing.T) {
	tempDir, err := GetSpacemeshTempDirectoryPath()
	assert.NoErr(t, err, "creating temp dir failed")
	aPath, err := GetFullDirectoryPath(tempDir)
	assert.Equal(t, tempDir, aPath, "Path is different")
	assert.Nil(t, err)
	assert.NoErr(t, DeleteAllTempFiles(), "removing dir failed")
}

func TestGetUserHomeDirectory(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")
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
	assert.NoErr(t, err, "getting current user failed")
	assert.Equal(t, usr.HomeDir, GetUserHomeDirectory(), "HOME is different")
}

func TestGetCanonicalPath(t *testing.T) {
	t.Parallel()
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

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

func TestGetSpacemeshDataDirectoryPath(t *testing.T) {
	t.Parallel()
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
	}{
		{usr, "~" + RootFolder + ".spacemesh"},
		{users["bob"], users["bob"].HomeDir + RootFolder + ".spacemesh"},
		{users["alice"], users["alice"].HomeDir + RootFolder + ".spacemesh"},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		actual, err := GetSpacemeshDataDirectoryPath()
		assert.Equal(t, testCase.expected, actual, "")
		assert.NoErr(t, err, "getting current user failed")
	}
	TearDownTestHooks()
}

func TestGetSpacemeshTempDirectoryPath(t *testing.T) {
	t.Parallel()
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
	}{
		{usr, "~" + RootFolder + ".spacemesh" + RootFolder + "temp"},
		{users["bob"], users["bob"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp"},
		{users["alice"], users["alice"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp"},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		actual, err := GetSpacemeshTempDirectoryPath()
		assert.Equal(t, testCase.expected, actual, "")
		assert.NoErr(t, err, "getting current user failed")
	}
	TearDownTestHooks()
}

func TestDeleteAllTempFiles(t *testing.T) {
	t.Parallel()
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
	}{
		{usr, "~" + RootFolder + ".spacemesh" + RootFolder + "temp"},
		{users["bob"], users["bob"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp"},
		{users["alice"], users["alice"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp"},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		err := DeleteAllTempFiles()
		assert.True(t, PathExists(testCase.expected), "expecting existence of path")
		assert.NoErr(t, err, "getting current user failed")
	}
	TearDownTestHooks()
}
