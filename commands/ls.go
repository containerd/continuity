package commands

import (
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/dustin/go-humanize"

	"github.com/spf13/cobra"
)

var LSCmd = &cobra.Command{
	Use:   "ls <manifest>",
	Short: "List the contents of the manifest.",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			log.Fatalln("please specify a manifest")
		}

		bm, err := readManifestFile(args[0])
		if err != nil {
			log.Fatalf("error reading manifest: %v", err)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)

		for _, entry := range bm.Entry {

			for _, path := range entry.Path {
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\n", os.FileMode(entry.Mode), entry.User, entry.Group, humanize.Bytes(uint64(entry.Size)), path)
			}
		}

		w.Flush()
	},
}
