package commands

import "github.com/spf13/cobra"

var VerifyCmd = &cobra.Command{
	Use:   "verify <manifest> <root>",
	Short: "Verify the manifest against the provided root",
	Run: func(cmd *cobra.Command, args []string) {

	},
}
