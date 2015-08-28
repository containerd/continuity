package commands

import "github.com/spf13/cobra"

var ApplyCmd = &cobra.Command{
	Use:   "apply <manifest> <root>",
	Short: "Apply the manifest to the provided root",
	Run: func(cmd *cobra.Command, args []string) {

	},
}
