package app

import (
	"github.com/UnrulyOS/go-unruly/app/config"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"os"
	"path/filepath"
)

// UnrulyApp data-related features

// Return true iff file exists and is accessible
func PathExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err != nil
}

func (app *UnrulyApp) GetUnrulyDataDirectoryPath() (string, error) {
	return filesystem.GetFullDirectoryPath(config.ConfigValues.DataFilePath)
}

// Return the os-specific path to the Unruly data folder
// Creates it and all subdirs on demand
func (app *UnrulyApp) ensureUnrulyDataDirectories() string {
	dataPath, err := app.GetUnrulyDataDirectoryPath()
	if err != nil {
		log.Error("Can't get or create unruly data folder")
		ExitApp <- true
	}

	// ensure sub folders exist - create them on demand
	app.GetAccountsDataDirectoryPath()
	app.GetLogsDataDirectoryPath()

	return dataPath
}

// Ensure a sub-directory exists
func (app *UnrulyApp) ensureDataSubDirectory(dirName string) (string, error) {
	dataPath, err := app.GetUnrulyDataDirectoryPath()
	if err != nil {
		log.Error("Failed to ensure data dir: %v", err)
		return "", err
	}

	pathName := filepath.Join(dataPath, dirName)
	path, err := filesystem.GetFullDirectoryPath(pathName)
	if err != nil {
		log.Error("Can't access unruly folder: %v", pathName)
		return "", err
	}
	return path, nil
}

func (app *UnrulyApp) GetAccountsDataDirectoryPath() string {
	path, err := app.ensureDataSubDirectory(config.AccountsDirectoryName)
	if err != nil {
		log.Error("Can't access unruly keys folder. %v", err)
		ExitApp <- true
	}
	return path
}

func (app *UnrulyApp) GetLogsDataDirectoryPath() string {
	path, err := app.ensureDataSubDirectory(config.LogDirectoryName)
	if err != nil {
		log.Error("Can't access unruly logs folder. %v", err)
		ExitApp <- true
	}
	return path
}
