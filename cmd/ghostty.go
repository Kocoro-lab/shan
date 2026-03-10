package cmd

import (
	"fmt"
	"os"

	"github.com/Kocoro-lab/shan/internal/tools"
	"github.com/spf13/cobra"
)

var ghosttyCmd = &cobra.Command{
	Use:   "ghostty",
	Short: "Ghostty terminal integration",
}

var workspaceCmd = &cobra.Command{
	Use:   "workspace <agent1> [agent2] ...",
	Short: "Open a Ghostty window with one tab per agent",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shanBin, err := os.Executable()
		if err != nil {
			shanBin = "shan"
		}
		script := tools.GhosttyWorkspaceScript(shanBin, args)
		if script == "" {
			return fmt.Errorf("ghostty workspace requires macOS")
		}
		return tools.ExecGhosttyScript(script)
	},
}

func init() {
	ghosttyCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(ghosttyCmd)
}
