//go:build linux

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
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/containerd/continuity"
	"github.com/containerd/continuity/cmd/continuity/continuityfs"
	"github.com/containerd/continuity/driver"
	"github.com/containerd/log"
	"github.com/spf13/cobra"
)

var MountCmd = &cobra.Command{
	Use:   "mount <mountpoint> [<manifest>] [<source directory>]",
	Short: "Mount the manifest to the provided mountpoint using content from a source directory",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 3 {
			log.L.Fatal("Must specify mountpoint, manifest, and source directory")
		}
		mountpoint := args[0]
		manifest, source := args[1], args[2]

		manifestName := filepath.Base(manifest)

		p, err := os.ReadFile(manifest)
		if err != nil {
			log.L.Fatalf("error reading manifest: %v", err)
		}

		m, err := continuity.Unmarshal(p)
		if err != nil {
			log.L.Fatalf("error unmarshaling manifest: %v", err)
		}

		driver, err := driver.NewSystemDriver()
		if err != nil {
			log.L.Fatal(err)
		}

		provider := continuityfs.NewFSFileContentProvider(source, driver)

		contfs, err := continuityfs.NewFSFromManifest(m, mountpoint, provider)
		if err != nil {
			log.L.Fatal(err)
		}

		c, err := fuse.Mount(
			mountpoint,
			fuse.ReadOnly(),
			fuse.FSName(manifestName),
			fuse.Subtype("continuity"),
		)
		if err != nil {
			log.L.Fatal(err)
		}

		errChan := make(chan error, 1)
		go func() {
			// TODO: Create server directory to use context
			err = fs.Serve(c, contfs)
			if err != nil {
				errChan <- err
			}
		}()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt)
		signal.Notify(sigChan, syscall.SIGTERM)

		select {
		case <-sigChan:
			log.L.Infof("Shutting down")
		case err = <-errChan:
		}

		go func() {
			if err := c.Close(); err != nil {
				log.L.Errorf("Unable to close connection %s", err)
			}
		}()

		// Wait for any inprogress requests to be handled
		time.Sleep(time.Second)

		log.L.Info("Attempting unmount")
		if err := fuse.Unmount(mountpoint); err != nil {
			log.L.Errorf("Error unmounting %s: %v", mountpoint, err)
		}

		// Handle server error
		if err != nil {
			log.L.Fatalf("Error serving fuse server: %v", err)
		}
	},
}
