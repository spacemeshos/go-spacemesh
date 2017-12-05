package app

import (
	"github.com/UnrulyOS/go-unruly/app/config"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"path/filepath"
)

// UnrulyApp data-related features

func (app *UnrulyApp) GetUnrulyDataDirectoryPath() (string, error) {
	return filesystem.GetFullDirectoryPath(config.ConfigValues.DataFilePath)
}

// Return the os-specific path to the Unruly data folder
// Creates it and all subfolders on demand

func (app *UnrulyApp) ensureUnrulyDataDirectories() string {
	dataPath, err := app.GetUnrulyDataDirectoryPath()
	if err != nil {
		log.Error("Can't get or create unruly data folder")
		ExitApp <- true
	}

	// ensure sub folders exist - create them on demand

	accountsPath := app.GetAccountsDataDirectoryPath()
	logsPath := app.GetLogsDataDirectoryPath()

	log.Info("Data dir: %s", dataPath)
	log.Info("Accounts data dir: %s", accountsPath)
	log.Info("Logs data dir: %s", logsPath)

	return dataPath
}

func (app *UnrulyApp) GetAccountsDataDirectoryPath() string {
	dataPath, err := app.GetUnrulyDataDirectoryPath()
	accounts := filepath.Join(dataPath, config.AccountsDirectoryName)
	acountsPath, err := filesystem.GetFullDirectoryPath(accounts)
	if err != nil {
		log.Error("Can't access unruly keys folder")
		ExitApp <- true
	}
	return acountsPath
}

func (app *UnrulyApp) GetLogsDataDirectoryPath() string {
	dataPath, err := app.GetUnrulyDataDirectoryPath()
	folder := filepath.Join(dataPath, config.LogDirectoryName)
	path, err := filesystem.GetFullDirectoryPath(folder)
	if err != nil {
		log.Error("Can't access unruly logs folder")
		ExitApp <- true
	}
	return path
}
