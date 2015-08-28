package continuity

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/docker/distribution/digest"
	pb "github.com/stevvooe/continuity/proto"
)

// BuildManifest creates the manifest for the root directory. includeFn should
// return nil for files that should be included in the manifest. The function
// is called with the unmodified arguments of filepath.Walk.
func BuildManifest(root string, includeFn filepath.WalkFunc) (*pb.Manifest, error) {
	entriesByPath := map[string]*pb.Entry{}
	hardlinks := map[hardlinkKey][]*pb.Entry{}

	gi, err := getGroupIndex()
	if err != nil {
		return nil, err
	}

	// normalize to absolute path
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}

	if err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if p == root {
			// skip the root
			return nil
		}

		sanitized, err := sanitize(root, p)
		if err != nil {
			return err
		}

		entry := pb.Entry{
			Path: []string{sanitized},
			Mode: uint32(fi.Mode()),
		}

		sysStat := fi.Sys().(*syscall.Stat_t)

		uid, gid := sysStat.Uid, sysStat.Gid

		u, err := user.LookupId(fmt.Sprint(uid))
		if err != nil {
			return err
		}
		entry.User = u.Username
		entry.Uid = fmt.Sprint(uid)
		entry.Group = gi.byGID[int(gid)].name
		entry.Gid = fmt.Sprint(gid)

		// TODO(stevvooe): Handle xattrs.
		// TODO(stevvooe): Handle ads.

		if fi.Mode().IsRegular() {
			entry.Size = uint64(fi.Size())

			// TODO(stevvooe): The nlinks technique is not always reliable on
			// certain filesystems. Must use the dev, inode to join them.
			if sysStat.Nlink < 2 {
				dgst, err := hashPath(p)
				if err != nil {
					return err
				}

				entry.Digest = append(entry.Digest, dgst.String())
			} else if sysStat.Nlink > 1 { // hard links
				// Properties of hard links:
				//	- nlinks > 1 (not all filesystems)
				//	- identical dev and inode number for two files
				//	- consider the file with the earlier ctime the "canonical" path
				//
				// How will this be done?
				//	- check nlinks to detect hard links
				//		-> add them to map by dev, inode
				//	- hard links are represented by a single entry with multiple paths
				//	- defer addition to entries until after all entries are seen
				key := hardlinkKey{dev: sysStat.Dev, inode: sysStat.Ino}

				// add the hardlink
				hardlinks[key] = append(hardlinks[key], &entry)

				// TODO(stevvooe): Possibly use os.SameFile here?

				return nil // hardlinks are postprocessed, so we exit
			}
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			// We handle relative links vs absolute links by including a
			// beginning slash for absolute links. Effectively, the bundle's
			// root is treated as the absolute link anchor.

			target, err := os.Readlink(p)
			if err != nil {
				return err
			}

			if filepath.IsAbs(target) {
				// When path is absolute, we make it relative to the bundle root.
				target, err = filepath.Rel(root, target)
				if err != nil {
					return err
				}

				// now make the target absolute, since we want to maintain that.
				target = filepath.Join("/", target)
			} else {
				// make sure the target is contained in the root.
				real := filepath.Join(p, target)
				if !strings.HasPrefix(real, root) {
					return fmt.Errorf("link refers to file outside of root: %q -> %q", p, target)
				}
			}

			entry.Target = target
		}

		if fi.Mode()&os.ModeNamedPipe != 0 {
			// Everything needed to rebuild a pipe is included in the mode.
		}

		if fi.Mode()&os.ModeDevice != 0 {
			// character and block devices merely need to recover the
			// major/minor device number.
			entry.Major = uint32(major(uint(sysStat.Rdev)))
			entry.Minor = uint32(minor(uint(sysStat.Rdev)))
		}

		if fi.Mode()&os.ModeSocket != 0 {
			return nil // sockets are skipped, no point
		}

		entriesByPath[p] = &entry

		return nil
	}); err != nil {
		return nil, err
	}

	// process the groups of hardlinks
	for pair, linked := range hardlinks {
		if len(linked) < 1 {
			return nil, fmt.Errorf("no hardlink entrys for dev, inode pair: %#v", pair)
		}

		// a canonical hardlink target is selected by sort position to ensure
		// the same file will always be used as the link target.
		sort.Sort(byPath(linked))

		canonical, rest := linked[0], linked[1:]

		dgst, err := hashPath(filepath.Join(root, canonical.Path[0]))
		if err != nil {
			return nil, err
		}

		// canonical gets appended like a regular file.
		canonical.Digest = append(canonical.Digest, dgst.String())
		entriesByPath[canonical.Path[0]] = canonical

		// process the links.
		for _, link := range rest {
			// a hardlink is a regular file with a target instead of a digest.
			// We can just set the target from the canonical path since
			// hardlinks are alwas
			canonical.Path = append(canonical.Path, link.Path...)
		}

		sort.Strings(canonical.Path)
	}

	var entries []*pb.Entry
	for _, entry := range entriesByPath {
		entries = append(entries, entry)
	}

	sort.Sort(byPath(entries))

	return &pb.Manifest{
		Entry: entries,
	}, nil
}

func ApplyManifest(root string, manifest *pb.Manifest) error {
	panic("not implemented")
}

// sanitize and clean the path relative to root.
func sanitize(root, p string) (string, error) {
	sanitized, err := filepath.Rel(root, p)
	if err != nil {
		return "", err
	}

	return filepath.Clean(sanitized), nil
}

// hardlinkKey provides a tuple-key for managing hardlinks.
type hardlinkKey struct {
	dev   int32
	inode uint64
}

func hashPath(p string) (digest.Digest, error) {
	digester := digest.Canonical.New() // TODO(stevvooe): Make this configurable.

	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(digester.Hash(), f); err != nil {
		return "", err
	}

	return digester.Digest(), nil

}

type byPath []*pb.Entry

func (bp byPath) Len() int           { return len(bp) }
func (bp byPath) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp byPath) Less(i, j int) bool { return bp[i].Path[0] < bp[j].Path[0] } // sort by first path entry.
