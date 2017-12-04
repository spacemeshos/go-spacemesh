package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	LoadConfigFileFlag = cli.StringFlag{
		Name:        "config, c",
		Usage:       "Load configuration from `FILE`",
		Value:       ConfigValues.ConfigFilePath,
		Destination: &ConfigValues.ConfigFilePath,
	}

	DataFolderPathFlag = cli.StringFlag{
		Name:        "dataFolder, df",
		Usage:       "Set root data folder`",
		Value:       ConfigValues.DataFilePath,
		Destination: &ConfigValues.DataFilePath,
	}
)
