// package multi_node_sim is an implementation of a framework running multiple nodes in same executable with fast hare
// implementation. allowing to create network samples in high rates
package main

import (
	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spf13/cobra"
	"os"
)

var (
	multiConfig = Config{}
)

// Config is the configuration struct for multi node sim
type Config struct {
	NumberOfNodes  int
	BlocksPerLayer int
	RunUntilLayer  uint32
	DbLocation     string
}

// AddCommands adds commands for multi node sim
func AddCommands(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&multiConfig.DbLocation,
		"dir", "d", "tmp/data", "directory to store output db")
	cmd.PersistentFlags().IntVarP(&multiConfig.BlocksPerLayer, "blocks", "b",
		10, "blocks per layer")
	cmd.PersistentFlags().IntVarP(&multiConfig.NumberOfNodes, "nodes", "n",
		5, "number of nodes")
	cmd.PersistentFlags().Uint32VarP(&multiConfig.RunUntilLayer, "layer", "l",
		50, "run until layer")
}

// Cmd is node simulator cmd
var Cmd = &cobra.Command{
	Use:   "run_sim",
	Short: "start simulation",
	Run: func(cmd *cobra.Command, args []string) {
		node.StartMultiNode(multiConfig.NumberOfNodes, multiConfig.BlocksPerLayer, multiConfig.RunUntilLayer, multiConfig.DbLocation)
	},
}

func init() {
	AddCommands(Cmd)
}

func main() {
	if err := Cmd.Execute(); err != nil {
		log.Error("%v", err)
		os.Exit(1)
	}
}
