package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/cmd/merge-nodes/internal"
)

var version string

func init() {
	rootCmd.Flags().StringP("from", "f", "",
		"The `data` folder to read identities from and merge into `to`")
	rootCmd.MarkFlagRequired("from")
	rootCmd.Flags().StringP("to", "t", "",
		"The `data` folder to write the merged node to. Can be an existing remote node or empty.")
	rootCmd.MarkFlagRequired("to")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "merge-nodes -f <dir> -t <dir>",
	Short: "Spacemesh Node Merger",
	Long: `Merge identities of two Spacemesh nodes into one.
The 'from' node will be merged into the 'to' node, leaving the 'from' node untouched.
The 'to' node can be an existing node or an empty folder.
Be sure to backup the 'to' node before running this command.
NOTE: both 'from' and 'to' nodes must be upgraded to the latest version before running this command.
NOTE: after upgrading and starting the nodes at least once, convert them to remote nodes before merging.`,
	Version: version,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg := zap.NewProductionConfig()
		cfg.Encoding = "console"
		dbLog, err := cfg.Build()
		if err != nil {
			log.Fatalf("create logger: %v", err)
		}
		defer dbLog.Sync()

		f := cmd.Flag("from").Value.String()
		t := cmd.Flag("to").Value.String()

		return internal.MergeDBs(cmd.Context(), dbLog, f, t)
	},
}
