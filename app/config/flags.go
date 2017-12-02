package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	LoadConfigFileFlag = cli.StringFlag{
		Name:  "config, c",
		Usage: "Load configuration from `FILE`",
		Value: DefaultConfig.ConfigFilePath,
	}

	DataFolderPath = cli.StringFlag{
		Name:  "--dataFolder, df",
		Usage: "Set root data folder`",
		Value: DefaultConfig.DataFilePath,
	}



)
