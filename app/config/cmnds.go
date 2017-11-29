package config

import (
	"fmt"
	"gopkg.in/urfave/cli.v1"
	"runtime"
)

func VersionCommand(appVersion string) cli.Command {
	return cli.Command{
		Name:      "version",
		Aliases:   []string{"v"},
		Usage:     "print versions",
		ArgsUsage: " ",
		Category:  "General commands",
		Action: func(c *cli.Context) error {
			fmt.Println("App Version:", appVersion)
			fmt.Println("Go Version:", runtime.Version())
			fmt.Println("OS:", runtime.GOOS)
			fmt.Println("Arch:", runtime.GOARCH)
			return nil
		},
	}
}
