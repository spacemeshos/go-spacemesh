package cmd

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/version"
	"github.com/spf13/cobra"
)

const (
	appUsage           = "the go-spacemesh node"
	appAuthor          = "The go-spacemesh authors"
	appAuthorEmail     = "info@spacemesh.io"
	appCopyrightNotice = "(c) 2017 The go-spacemesh Authors"
)

// VersionCmd returns the current version of spacemesh
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Version)
	},
}

func init() {
	RootCmd.AddCommand(VersionCmd)
}
