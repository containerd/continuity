package commands

import (
	"log"
	"os"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/cobra"
	"github.com/stevvooe/continuity"
)

var (
	buildCmdConfig struct {
		format string
	}

	BuildCmd = &cobra.Command{
		Use:   "build <root>",
		Short: "Build a manifest for the provided root",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalln("please specify a root")
			}

			m, err := continuity.BuildManifest(args[0], nil)
			if err != nil {
				log.Fatalf("error generating manifest: %v", err)
			}

			p, err := proto.Marshal(m)
			if err != nil {
				log.Fatalf("error marshing manifest: %v", err)
			}

			os.Stdout.Write(p)
		},
	}
)

func init() {
	BuildCmd.Flags().StringVar(&buildCmdConfig.format, "format", "pb", "specify the output format of the manifest")
}
