package config

import (
	"fmt"
	"gopkg.in/urfave/cli.v1"
	"runtime"
)

var (
// todo: add all command vars here (commands w no need for factory)
)

// Command factory function - this is an example for an app cli command
// to test try `/go-spacemesh version`
func NewVersionCommand(appVersion, branch, commit string) cli.Command {
	return cli.Command{
		Name:      "version",
		Aliases:   []string{"v"},
		Usage:     "print versions",
		ArgsUsage: " ",
		Category:  "General commands",
		Action: func(c *cli.Context) error {
			fmt.Println("App Version:", appVersion)
			fmt.Printf("Git branch: %s. Commit: %s\n", branch, commit)
			fmt.Println("Go Version:", runtime.Version())
			fmt.Println("OS:", runtime.GOOS)
			fmt.Println("Arch:", runtime.GOARCH)
			return nil
		},
	}
}
