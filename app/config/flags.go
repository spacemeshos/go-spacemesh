package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	LoadConfigFileFlag = cli.StringFlag{
		Name:  "config, c",
		Usage: "Load configuration from `FILE`",
		Destination: &ConfigValues.ConfigFilePath,
	}

	DataFolderPathFlag = cli.StringFlag{
		Name:  "dataFolder, df",
		Usage: "Set root data folder`",
		Destination: &ConfigValues.DataFilePath,
	}

)
