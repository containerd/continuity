package commands

import (
	"fmt"
	"log"
	"os"

	"github.com/containerd/continuity"
	"github.com/spf13/cobra"
)

var (
	buildCmdConfig struct {
		format string
	}

	marshalers = map[string]func(*continuity.Manifest) ([]byte, error){
		"pb": continuity.Marshal,
		continuity.MediaTypeManifestV0Protobuf: continuity.Marshal,
		"json": continuity.MarshalJSON,
		continuity.MediaTypeManifestV0JSON: continuity.MarshalJSON,
	}

	BuildCmd = &cobra.Command{
		Use:   "build <root>",
		Short: "Build a manifest for the provided root",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalln("please specify a root")
			}

			ctx, err := continuity.NewContext(args[0])
			if err != nil {
				log.Fatalf("error creating path context: %v", err)
			}

			m, err := continuity.BuildManifest(ctx)
			if err != nil {
				log.Fatalf("error generating manifest: %v", err)
			}

			marshaler, ok := marshalers[buildCmdConfig.format]
			if !ok {
				log.Fatalf("unknown format %s", buildCmdConfig.format)
			}

			p, err := marshaler(m)
			if err != nil {
				log.Fatalf("error marshalling manifest as %s: %v",
					buildCmdConfig.format, err)
			}

			if _, err := os.Stdout.Write(p); err != nil {
				log.Fatalf("error writing to stdout: %v", err)
			}
		},
	}
)

func init() {
	BuildCmd.Flags().StringVar(&buildCmdConfig.format, "format", "pb",
		fmt.Sprintf("specify the output format of the manifest (\"pb\"|%q|\"json\"|%q)",
			continuity.MediaTypeManifestV0Protobuf, continuity.MediaTypeManifestV0JSON))
}
