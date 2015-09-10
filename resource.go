package continuity

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"syscall"

	"github.com/docker/distribution/digest"
)

// TODO(stevvooe): A record based model, somewhat sketched out at the bottom
// of this file, will be more flexible.

type Resource interface {
	// Path provides the primary resource path relative to the bundle root. In
	// cases where resources have more than one path, such as with hard links,
	// this will return the primary path, which is often just the first entry.
	Path() string

	// Mode returns the
	Mode() os.FileMode

	UID() string
	GID() string
}

type XAttrer interface {
	XAttrs() map[string][]byte
}

type RegularFile interface {
	Resource
	XAttrer

	// Paths returns all paths of the regular file, including the primary path
	// returned by Resource.Path. If len(Paths()) > 1, the resource is a hard
	// link.
	Paths() []string

	Size() int64
	Digests() []digest.Digest
}

// Merge two or more RegularFile fs into new file. Typically, this should be
// used to merge regular files as hardlinks. If the files are not identical,
// other than Paths and Digests, the merge will fail and an error will be
// returned.
func Merge(fs ...RegularFile) (RegularFile, error) {
	if len(fs) < 1 {
		return nil, fmt.Errorf("please provide a file to merge")
	}

	if len(fs) == 1 {
		return fs[0], nil
	}

	var paths []string
	var digests []digest.Digest
	bypath := map[string][]RegularFile{}

	// The attributes are all compared against the first to make sure they
	// agree before adding to the above collections. If any of these don't
	// correctly validate, the merge fails.
	prototype := fs[0]
	xattrs := make(map[string][]byte, len(prototype.XAttrs()))

	// initialize xattrs for use below. All files must have same xattrs.
	for attr, value := range prototype.XAttrs() {
		xattrs[attr] = value
	}

	for _, f := range fs {
		if f.Mode() != prototype.Mode() {
			return nil, fmt.Errorf("modes do not match: %v != %v", f.Mode(), prototype.Mode())
		}

		if f.UID() != prototype.UID() {
			return nil, fmt.Errorf("uid does not match: %v != %v", f.UID(), prototype.UID())
		}

		if f.GID() != prototype.GID() {
			return nil, fmt.Errorf("gid does not match: %v != %v", f.GID(), prototype.GID())
		}

		if f.Size() != prototype.Size() {
			return nil, fmt.Errorf("size does not match: %v != %v", f.Size(), prototype.Size())
		}

		fxattrs := f.XAttrs()
		if !reflect.DeepEqual(fxattrs, xattrs) {
			return nil, fmt.Errorf("resource %q xattrs do not match: %v != %v", fxattrs, xattrs)
		}

		for _, p := range f.Paths() {
			pfs, ok := bypath[p]
			if !ok {
				// ensure paths are unique by only appending on a new path.
				paths = append(paths, p)
			}

			bypath[p] = append(pfs, f)
		}

		digests = append(digests, f.Digests()...)
	}

	sort.Stable(sort.StringSlice(paths))

	var err error
	digests, err = uniqifyDigests(digests...)
	if err != nil {
		return nil, err
	}

	// Choose a "canonical" file. Really, it is just the first file to sort
	// against. We also effectively select the very first digest as the
	// "canonical" one for this file.
	first := bypath[paths[0]][0]
	canonical := &regularFile{
		resource: resource{
			paths:  paths,
			mode:   first.Mode(),
			uid:    first.UID(),
			gid:    first.GID(),
			xattrs: xattrs,
		},
		size:    first.Size(),
		digests: digests,
	}

	return canonical, nil
}

type Directory interface {
	Resource
	XAttrer

	// Directory is a no-op method to identify directory objects by interface.
	Directory()
}

type SymLink interface {
	Resource

	// Target returns the target of the symlink contained in the .
	Target() string
}

type NamedPipe interface {
	Resource

	// Pipe is a no-op method to allow consistent resolution of NamedPipe
	// interface.
	Pipe()
}

// uniqifyDigests sorts and uniqifies the provided digest, ensuring that the
// digests are not repeated and no two digests with the same algorithm have
// different values. Because a stable sort is used, this has the effect of
// "zipping" digest collections from multiple resources.
func uniqifyDigests(digests ...digest.Digest) ([]digest.Digest, error) {
	sort.Stable(digestSlice(digests)) // stable sort is important for the behavior here.
	seen := map[digest.Digest]struct{}{}
	algs := map[digest.Algorithm][]digest.Digest{} // detect different digests.

	var out []digest.Digest
	// uniqify the digests
	for _, d := range digests {
		if _, ok := seen[d]; ok {
			continue
		}

		seen[d] = struct{}{}
		algs[d.Algorithm()] = append(algs[d.Algorithm()], d)

		if len(algs[d.Algorithm()]) > 1 {
			return nil, fmt.Errorf("conflicting digests for %v found", d.Algorithm())
		}

		out = append(out, d)
	}

	return out, nil
}

type resource struct {
	paths    []string
	mode     os.FileMode
	uid, gid string
	xattrs   map[string][]byte
}

var _ Resource = &resource{}

// newBaseResource returns a *resource, populated with data from p and fi,
// where p will be populated directly.
func newBaseResource(p string, fi os.FileInfo) (*resource, error) {
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		// TODO(stevvooe): This may not be a hard error for all platforms.
		return nil, fmt.Errorf("unable to resolve syscall.Stat_t from (os.FileInfo).Sys(): %#v", fi)
	}

	return &resource{
		paths: []string{p},
		mode:  fi.Mode(),

		// TODO(stevvooe): This need to be resolved for the container's root,
		// where here we are really getting the host OS's value. We need to
		// allow this be passed in and fixed up to make these uid/gid mappings
		// portable.
		uid: fmt.Sprint(sys.Uid),
		gid: fmt.Sprint(sys.Gid),

		// NOTE(stevvooe): Population of shared xattrs field is deferred to
		// the resource types that populate it. Since they are a property of
		// the context, they must set there.
	}, nil
}

func (r *resource) Path() string {
	if len(r.paths) < 1 {
		return ""
	}

	return r.paths[0]
}

func (r *resource) Mode() os.FileMode {
	return r.mode
}

func (r *resource) UID() string {
	return r.uid
}

func (r *resource) GID() string {
	return r.gid
}

type regularFile struct {
	resource
	size    int64
	digests []digest.Digest
}

var _ RegularFile = &regularFile{}

// newRegularFile returns the RegularFile, using the populated base resource
// and one or more digests of the content.
func newRegularFile(p string, fi os.FileInfo, base *resource, dgsts ...digest.Digest) (RegularFile, error) {
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}

	// make our own copy of digests
	ds := make([]digest.Digest, 0, len(dgsts))
	copy(ds, dgsts)

	return &regularFile{
		resource: *base,
		size:     fi.Size(),
		digests:  ds,
	}, nil
}

func (rf *regularFile) Paths() []string {
	paths := make([]string, len(rf.paths))
	copy(paths, rf.paths)
	return paths
}

func (rf *regularFile) Size() int64 {
	return rf.size
}

func (rf *regularFile) Digests() []digest.Digest {
	digests := make([]digest.Digest, len(rf.digests))
	copy(digests, rf.digests)
	return digests
}

func (rf *regularFile) XAttrs() map[string][]byte {
	xattrs := make(map[string][]byte, len(rf.xattrs))

	for attr, value := range rf.xattrs {
		xattrs[attr] = append(xattrs[attr], value...)
	}

	return xattrs
}

type directory struct {
	resource
}

var _ Directory = &directory{}

func newDirectory(p string, fi os.FileInfo, base *resource) (Directory, error) {
	if !fi.Mode().IsDir() {
		return nil, fmt.Errorf("not a directory")
	}

	return &directory{
		resource: *base,
	}, nil
}

func (d *directory) Directory() {}

func (d *directory) XAttrs() map[string][]byte {
	xattrs := make(map[string][]byte, len(d.xattrs))

	for attr, value := range d.xattrs {
		xattrs[attr] = append(xattrs[attr], value...)
	}

	return xattrs
}

type symLink struct {
	resource
	target string
}

var _ SymLink = &symLink{}

func newSymLink(p string, fi os.FileInfo, base *resource, target string) (SymLink, error) {
	return &symLink{
		resource: *base,
		target:   target,
	}, nil
}

func (l *symLink) Target() string {
	return l.target
}

type namedPipe struct {
	resource
}

var _ NamedPipe = &namedPipe{}

func newNamedPipe(p string, fi os.FileInfo, base *resource) (NamedPipe, error) {
	return &namedPipe{
		resource: *base,
	}, nil
}

func (np *namedPipe) Pipe() {}

type resourceCharDevice struct {
	Major, Minor int
}

type resourceBlockDevice struct {
	Major, Minor int
}

type resourceByPath []Resource

func (bp resourceByPath) Len() int           { return len(bp) }
func (bp resourceByPath) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp resourceByPath) Less(i, j int) bool { return bp[i].Path() < bp[j].Path() }

type digestSlice []digest.Digest

func (p digestSlice) Len() int           { return len(p) }
func (p digestSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p digestSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// NOTE(stevvooe): An alternative model that supports inline declaration.
// Convenient for unit testing where inline declarations may be desirable but
// creates an awkward API for the standard use case.

// type ResourceKind int

// const (
// 	ResourceRegularFile = iota + 1
// 	ResourceDirectory
// 	ResourceSymLink
// 	Resource
// )

// type Resource struct {
// 	Kind         ResourceKind
// 	Paths        []string
// 	Mode         os.FileMode
// 	UID          string
// 	GID          string
// 	Size         int64
// 	Digests      []digest.Digest
// 	Target       string
// 	Major, Minor int
// 	XAttrs       map[string][]byte
// }

// type RegularFile struct {
// 	Paths   []string
//  Size 	int64
// 	Digests []digest.Digest
// 	Perm    os.FileMode // os.ModePerm + sticky, setuid, setgid
// }
