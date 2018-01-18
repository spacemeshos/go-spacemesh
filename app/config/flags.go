package config

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

var (
	LoadConfigFileFlag = cli.StringFlag{
		Name:        "config, c",
		Usage:       "Load configuration from `FILE`",
		Value:       ConfigValues.ConfigFilePath,
		Destination: &ConfigValues.ConfigFilePath,
		EnvVar: "CONFIG_FILE",
	}

	DataFolderPathFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "data-folder",
		Usage:       "Set root data folder`",
		Value:       ConfigValues.DataFilePath,
		Destination: &ConfigValues.DataFilePath,
		EnvVar:"DATA_FOLDER",
	})
)
