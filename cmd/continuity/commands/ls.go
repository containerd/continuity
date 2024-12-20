/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package commands

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/containerd/continuity/proto"
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

		for _, entry := range bm.Resource {
			user, group := getUserGroup(entry)
			for _, path := range entry.Path {
				if os.FileMode(entry.Mode)&os.ModeSymlink != 0 {
					_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v -> %v\n", os.FileMode(entry.Mode), user, group, humanize.Bytes(uint64(entry.Size)), path, entry.Target) //nolint:unconvert
				} else {
					_, _ = fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\n", os.FileMode(entry.Mode), user, group, humanize.Bytes(uint64(entry.Size)), path) //nolint:unconvert
				}
			}
		}

		_ = w.Flush()
	},
}

func getUserGroup(entry *proto.Resource) (user, group string) {
	user = entry.User //nolint:staticcheck // ignore SA1019: entry.User is deprecated.
	if user == "" {
		user = strconv.FormatInt(entry.Uid, 10)
	}
	group = entry.Group //nolint:staticcheck // ignore SA1019: entry.Group is deprecated.
	if group == "" {
		group = strconv.FormatInt(entry.Gid, 10)
	}
	return user, group
}
