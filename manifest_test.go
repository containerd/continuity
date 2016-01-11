package continuity

import (
	_ "crypto/sha256"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/docker/distribution/digest"
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
	//		- what about sticky bits?
	// 2. Build the manifest.
	// 3. Verify expected result.
	testResources := []dresource{
		{
			path: "a",
			mode: 0644,
		},
		{
			kind:   rhardlink,
			path:   "a-hardlink",
			target: "a",
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		{
			kind:   rhardlink,
			path:   "b/a-hardlink",
			target: "a",
		},
		{
			path: "b/a",
			mode: 0600 | os.ModeSticky,
		},
		{
			kind: rdirectory,
			path: "c",
			mode: 0755,
		},
		{
			path: "c/a",
			mode: 0644,
		},
		{
			kind:   rrelsymlink,
			path:   "c/ca-relsymlink",
			mode:   0600,
			target: "a",
		},
		{
			kind:   rrelsymlink,
			path:   "c/a-relsymlink",
			mode:   0600,
			target: "../a",
		},
		{
			kind:   rabssymlink,
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
			kind: rnamedpipe,
			path: "fifo",
			mode: 0666 | os.ModeNamedPipe,
		},

		{
			kind: rdirectory,
			path: "/dev",
			mode: 0755,
		},

		// NOTE(stevvooe): Below here, we add a few simple character devices.
		// Block devices are untested but should be nearly the same as
		// character devices.
		// devNullResource,
		// devZeroResource,
	}

	root, err := ioutil.TempDir("", "continuity-test-")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}

	defer os.RemoveAll(root)

	generateTestFiles(t, root, testResources)

	ctx, err := NewContext(root)
	if err != nil {
		t.Fatalf("error getting context: %v", err)
	}

	m, err := BuildManifest(ctx)
	if err != nil {
		t.Fatalf("error building manifest: %v, %#T", err, err)
	}

	MarshalText(os.Stdout, m)
	t.Fail() // TODO(stevvooe): Actually test input/output matches
}

// TODO(stevvooe): At this time, we have a nice testing framework to define
// and build resources. This will likely be a pre-cursor to the packages
// public interface.
type kind int

func (k kind) String() string {
	switch k {
	case rfile:
		return "file"
	case rdirectory:
		return "directory"
	case rhardlink:
		return "hardlink"
	case rchardev:
		return "chardev"
	case rnamedpipe:
		return "namedpipe"
	}

	panic(fmt.Sprintf("unknown kind: %v", int(k)))
}

const (
	rfile kind = iota
	rdirectory
	rhardlink
	rrelsymlink
	rabssymlink
	rchardev
	rnamedpipe
)

type dresource struct {
	kind         kind
	path         string
	mode         os.FileMode
	target       string // hard/soft link target
	digest       digest.Digest
	major, minor int
}

func generateTestFiles(t *testing.T, root string, resources []dresource) {
	for i, resource := range resources {
		p := filepath.Join(root, resource.path)
		switch resource.kind {
		case rfile:
			size := rand.Intn(4 << 20)
			d := make([]byte, size)
			randomBytes(d)
			dgst := digest.FromBytes(d)
			resources[i].digest = dgst

			// this relies on the proper directory parent being defined.
			if err := ioutil.WriteFile(p, d, resource.mode); err != nil {
				t.Fatalf("error writing %q: %v", p, err)
			}
		case rdirectory:
			if err := os.Mkdir(p, resource.mode); err != nil {
				t.Fatalf("error creating directory %q: %v", err)
			}
		case rhardlink:
			target := filepath.Join(root, resource.target)
			if err := os.Link(target, p); err != nil {
				t.Fatalf("error creating hardlink: %v", err)
			}
		case rrelsymlink:
			if err := os.Symlink(resource.target, p); err != nil {
				t.Fatalf("error creating symlink: %v", err)
			}
		case rabssymlink:
			// for absolute links, we join with root.
			target := filepath.Join(root, resource.target)

			if err := os.Symlink(target, p); err != nil {
				t.Fatalf("error creating symlink: %v", err)
			}
		case rchardev, rnamedpipe:
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
