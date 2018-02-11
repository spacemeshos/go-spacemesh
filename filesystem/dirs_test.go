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
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
		error    bool
	}{
		{usr, "~" + RootFolder + ".spacemesh", false},
		{users["bob"], users["bob"].HomeDir + RootFolder + ".spacemesh", true},
		{users["alice"], users["alice"].HomeDir + RootFolder + ".spacemesh", true},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		actual, err := GetSpacemeshDataDirectoryPath()
		assert.Equal(t, testCase.expected, actual, "")
		assert.Equal(t, err != nil, testCase.error, "")
	}
	TearDownTestHooks()
}

func TestGetSpacemeshTempDirectoryPath(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
		error    bool
	}{
		{usr, "~" + RootFolder + ".spacemesh" + RootFolder + "temp", false},
		{users["bob"], "", true},
		{users["alice"], "", true},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		actual, err := GetSpacemeshTempDirectoryPath()
		assert.Equal(t, testCase.expected, actual, "")
		assert.Equal(t, err != nil, testCase.error, "")
	}
	TearDownTestHooks()
}

func TestDeleteAllTempFiles(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		expected string
		error    bool
		exist    bool
	}{
		{usr, "~" + RootFolder + ".spacemesh" + RootFolder + "temp", false, true},
		{users["bob"], users["bob"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp", true, false},
		{users["alice"], users["alice"].HomeDir + RootFolder + ".spacemesh" + RootFolder + "temp", true, false},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.user.HomeDir)
		err := DeleteAllTempFiles()
		assert.Equal(t, err != nil, testCase.error, "")
		assert.Equal(t, testCase.exist, PathExists(testCase.expected), "")
	}
	TearDownTestHooks()
}

func TestGetLogsDataDirectoryPath(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user  *user.User
		home  string
		error bool
	}{
		{users["alice"], "", false},
		{users["bob"], "", false},
		{usr, usr.HomeDir, false},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.home)
		actual, err := GetLogsDataDirectoryPath()
		assert.Equal(t, testCase.error, err != nil, "")
		dir, err := GetFullDirectoryPath(actual)
		assert.NoErr(t, err, "")
		assert.Equal(t, actual, dir, "")
	}

	TearDownTestHooks()
	usr, err = currentUser()
	assert.NoErr(t, err, "getting current user failed")
	aPath, err := GetLogsDataDirectoryPath()
	assert.NoErr(t, err, "getting logs data directory failed")
	dir, err := GetFullDirectoryPath(aPath)
	assert.NoErr(t, err, "Path is different")
	assert.Equal(t, aPath, dir, "Logs directory is different")
}

func TestGetAccountsDataDirectoryPath(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user  *user.User
		home  string
		error bool
	}{
		{users["alice"], "", false},
		{users["bob"], "", false},
		{usr, usr.HomeDir, false},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.home)
		actual, err := GetAccountsDataDirectoryPath()
		assert.Equal(t, testCase.error, err != nil, "")
		dir, err := GetFullDirectoryPath(actual)
		assert.NoErr(t, err, "")
		assert.Equal(t, actual, dir, "")
	}

	TearDownTestHooks()
	usr, err = currentUser()
	assert.NoErr(t, err, "getting current user failed")
	aPath, err := GetAccountsDataDirectoryPath()
	assert.NoErr(t, err, "getting accounts data directory failed")
	dir, err := GetFullDirectoryPath(aPath)
	assert.NoErr(t, err, "Path is different")
	assert.Equal(t, aPath, dir, "Accounts directory is different")
}

func TestEnsureSpacemeshDataDirectories(t *testing.T) {
	users := TestUsers()
	SetupTestHooks(users)
	usr, err := currentUser()
	assert.NoErr(t, err, "getting current user failed")

	testCases := []struct {
		user     *user.User
		home     string
		expected string
		error    bool
	}{
		{users["alice"], "", "~" + RootFolder + ".spacemesh", false},
		{users["bob"], "", "~" + RootFolder + ".spacemesh", false},
		{usr, usr.HomeDir, "~" + RootFolder + ".spacemesh", false},
	}

	for _, testCase := range testCases {
		os.Setenv("HOME", testCase.home)
		actual, err := EnsureSpacemeshDataDirectories()
		assert.Equal(t, testCase.error, err != nil, "")
		assert.Equal(t, actual, testCase.expected, "")
	}

	TearDownTestHooks()
	usr, err = currentUser()
	assert.NoErr(t, err, "getting current user failed")
	aPath, err := EnsureSpacemeshDataDirectories()
	assert.NoErr(t, err, "")
	assert.Equal(t, aPath, usr.HomeDir+RootFolder+".spacemesh", "")
}
