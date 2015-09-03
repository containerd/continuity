package continuity

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/docker/distribution/digest"

	"github.com/golang/protobuf/proto"
)

// Hard things:
//  1. Groups/gid - no standard library support.
//  2. xattrs - must choose package to provide this.
//  3. ADS - no clue where to start.

func TestWalkFS(t *testing.T) {
	rand.Seed(1)

	// Testing:
	// 1. Setup different files:
	//		- links
	//			- sibling directory - relative
	//			- sibling directory - absolute
	//			- parent directory - absolute
	//			- parent directory - relative
	//		- illegal links
	//			- parent directory - relative, out of root
	//			- parent directory - absolute, out of root
	//		- regular files
	//		- character devices
	// 2. Build the manifest.
	// 3. Verify expected result.

	testResources := []resource{
		{
			path: "a",
			mode: 0644,
		},
		{
			kind:   hardlink,
			path:   "a-hardlink",
			target: "a",
		},
		{
			kind: directory,
			path: "b",
			mode: 0755,
		},
		{
			kind:   hardlink,
			path:   "b/a-hardlink",
			target: "a",
		},
		{
			path: "b/a",
			mode: 0600,
		},
		{
			kind: directory,
			path: "c",
			mode: 0755,
		},
		{
			path: "c/a",
			mode: 0644,
		},
		{
			kind:   relsymlink,
			path:   "c/ca-relsymlink",
			mode:   0600,
			target: "a",
		},
		{
			kind:   relsymlink,
			path:   "c/a-relsymlink",
			mode:   0600,
			target: "../a",
		},
		{
			kind:   abssymlink,
			path:   "c/a-abssymlink",
			mode:   0600,
			target: "a",
		},
		// TODO(stevvooe): Make sure we can test this case and get proper
		// errors when it is encountered.
		// {
		// 	// create a bad symlink and make sure we don't include it.
		// 	kind:   relsymlink,
		// 	path:   "c/a-badsymlink",
		// 	mode:   0600,
		// 	target: "../../..",
		// },

		// TODO(stevvooe): Must add tests for xattrs, with symlinks,
		// directorys and regular files.

		{
			kind: namedpipe,
			path: "fifo",
			mode: 0666 | os.ModeNamedPipe,
		},

		// NOTE(stevvooe): Below here, we add a few simple character devices.
		// Block devices are untested but should be nearly the same as
		// character devices.
		devNullResource,
		devZeroResource,
	}

	root, err := ioutil.TempDir("", "continuity-test-")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}

	defer os.RemoveAll(root)

	generateTestFiles(t, root, testResources)

	bm, err := BuildManifest(root, nil)
	if err != nil {
		t.Fatalf("error building manifest: %v", err)
	}

	proto.MarshalText(os.Stdout, bm)
	t.Fail() // TODO(stevvooe): Actually test input/output matches
}

// TODO(stevvooe): At this time, we have a nice testing framework to define
// and build resources. This will likely be a pre-cursor to the packages
// public interface.

type kind int

func (k kind) String() string {
	switch k {
	case file:
		return "file"
	case directory:
		return "directory"
	case hardlink:
		return "hardlink"
	case chardev:
		return "chardev"
	case namedpipe:
		return "namedpipe"
	}

	panic(fmt.Sprintf("unknown kind: %v", int(k)))
}

const (
	file kind = iota
	directory
	hardlink
	relsymlink
	abssymlink
	chardev
	namedpipe
)

type resource struct {
	kind         kind
	path         string
	mode         os.FileMode
	target       string // hard/soft link target
	digest       digest.Digest
	major, minor int
}

func generateTestFiles(t *testing.T, root string, resources []resource) {
	for i, resource := range resources {
		p := filepath.Join(root, resource.path)
		switch resource.kind {
		case file:
			size := rand.Intn(4 << 20)
			d := make([]byte, size)
			randomBytes(d)
			dgst, err := digest.FromBytes(d)
			if err != nil {
				t.Fatalf("error digesting %q: %v", p, err)
			}
			resources[i].digest = dgst

			// this relies on the proper directory parent being defined.
			if err := ioutil.WriteFile(p, d, resource.mode); err != nil {
				t.Fatalf("error writing %q: %v", p, err)
			}
		case directory:
			if err := os.Mkdir(p, resource.mode); err != nil {
				t.Fatalf("error creating directory %q: %v", err)
			}
		case hardlink:
			target := filepath.Join(root, resource.target)
			if err := os.Link(target, p); err != nil {
				t.Fatalf("error creating hardlink: %v", err)
			}
		case relsymlink:
			if err := os.Symlink(resource.target, p); err != nil {
				t.Fatalf("error creating symlink: %v", err)
			}
		case abssymlink:
			// for absolute links, we join with root.
			target := filepath.Join(root, resource.target)

			if err := os.Symlink(target, p); err != nil {
				t.Fatalf("error creating symlink: %v", err)
			}
		case chardev, namedpipe:
			if err := mknod(p, resource.mode, resource.major, resource.minor); err != nil {
				t.Fatalf("error creating device %q: %v", p, err)
			}
		default:
			t.Fatalf("unknown resource type: %v", resource.kind)
		}

	}

	// log the test root for future debugging
	if err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			t.Log(fi.Mode(), p, "->", target)
		} else {
			t.Log(fi.Mode(), p)
		}

		return nil
	}); err != nil {
		t.Fatalf("error walking created root: %v", err)
	}

	cmd := exec.Command("tree", root)
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("error running tree command: %v", err)
	}

}

func randomBytes(p []byte) {
	for i := range p {
		p[i] = byte(rand.Intn(1<<8 - 1))
	}
}
