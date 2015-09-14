package continuity

import (
	"os"
	"sort"

	pb "github.com/stevvooe/continuity/proto"
)

// BuildManifest creates the manifest for the given context
func BuildManifest(ctx Context) (*pb.Manifest, error) {
	resourcesByPath := map[string]Resource{}
	hardlinks := newHardlinkManager()

	if err := ctx.Walk(func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		sanitized, err := ctx.Sanitize(p)
		if err != nil {
			return err
		}
		if sanitized == "." {
			// skip the root
			return nil
		}

		resource, err := ctx.Resource(sanitized, fi)
		if err != nil {
			if err == ErrNotFound {
				return nil
			}
			return err
		}

		// add to the hardlink manager
		if err := hardlinks.Add(p, fi, resource); err == nil {
			// Resource has been accepted by hardlink manager so we don't add
			// it to the resourcesByPath until we merge at the end.
			return nil
		} else if err != errNotAHardLink {
			// handle any other case where we have a proper error.
			return err
		}

		resourcesByPath[p] = resource

		return nil
	}); err != nil {
		return nil, err
	}

	// merge and post-process the hardlinks.
	hardlinked, err := hardlinks.Merge()
	if err != nil {
		return nil, err
	}

	for _, resource := range hardlinked {
		resourcesByPath[resource.Path()] = resource
	}

	var entries []*pb.Resource
	for _, resource := range resourcesByPath {
		entry := &pb.Resource{
			Path: []string{resource.Path()},
			Mode: uint32(resource.Mode()),
			Uid:  resource.UID(),
			Gid:  resource.GID(),
		}

		if xattrer, ok := resource.(XAttrer); ok {
			entry.Xattr = xattrer.XAttrs()
		}

		switch r := resource.(type) {
		case RegularFile:
			entry.Path = r.Paths()
			entry.Size = uint64(r.Size())

			for _, dgst := range r.Digests() {
				entry.Digest = append(entry.Digest, dgst.String())
			}
		case SymLink:
			entry.Target = r.Target()
		}

		// enforce a few stability guarantees that may not be provided by the
		// resource implementation.
		sort.Strings(entry.Path)

		entries = append(entries, entry)
	}

	sort.Sort(byPath(entries))

	return &pb.Manifest{
		Resource: entries,
	}, nil
}

func ApplyManifest(root string, manifest *pb.Manifest) error {
	panic("not implemented")
}

type byPath []*pb.Resource

func (bp byPath) Len() int           { return len(bp) }
func (bp byPath) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp byPath) Less(i, j int) bool { return bp[i].Path[0] < bp[j].Path[0] } // sort by first path entry.
