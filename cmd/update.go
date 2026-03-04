package cmd

import (
	"fmt"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/cobra"

	"github.com/Kocoro-lab/shan/internal/update"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update shan to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("shan %s (%s)\n", Version, update.PlatformInfo())
		fmt.Println("Checking for updates...")

		// Homebrew detection
		exe, err := selfupdate.ExecutablePath()
		if err == nil && update.IsHomebrewPath(exe) {
			fmt.Println("Installed via Homebrew. Run: brew upgrade shan")
			return nil
		}

		newVersion, err := update.DoUpdate(Version)
		if err != nil {
			return fmt.Errorf("%v", err)
		}
		fmt.Printf("Updated to v%s. Restart to use new version.\n", newVersion)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
