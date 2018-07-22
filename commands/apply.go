package commands

import (
	"log"

	"github.com/containerd/continuity"
	"github.com/spf13/cobra"
)

var ApplyCmd = &cobra.Command{
	Use:   "apply <root> [<manifest>]",
	Short: "Apply the manifest to the provided root",
	Run: func(cmd *cobra.Command, args []string) {
		root, path := args[0], args[1]

		m, err := readManifest(path)
		if err != nil {
			log.Fatal(err)
		}

		ctx, err := continuity.NewContext(root)
		if err != nil {
			log.Fatalf("error getting context: %v", err)
		}

		if err := continuity.ApplyManifest(ctx, m); err != nil {
			log.Fatalf("error applying manifest: %v", err)
		}
	},
}
