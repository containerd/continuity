package continuity

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/distribution/digest"
)

var (
	ErrNotFound = fmt.Errorf("not found")
)

type Context interface {
	Verify(Resource) error
	Resource(string, os.FileInfo) (Resource, error)
	Sanitize(string) (string, error)
	Walk(filepath.WalkFunc) error
}

// context represents a file system context for accessing resources.
// Generally, all path qualified access and system considerations should land
// here.
type context struct {
	root string

	// TODO(stevvooe): Define a "context driver" type that can be used to
	// switch out the backing context implementation. This should handle
	// paths, digesting files, opening files, resolving system specific
	// attributes in addition to being a place where we can isolate system
	// specific operations.
}

// NewContext returns a Context associated with root.
func NewContext(root string) (Context, error) {
	// normalize to absolute path
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}

	// Check the root directory. Need to be a little careful here. We are
	// allowing a link for now, but this may have odd behavior when
	// canonicalizing paths. As long as all files are opened through the link
	// path, this should be okay.
	fi, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, &os.PathError{Op: "NewContext", Path: root, Err: os.ErrInvalid}
	}

	return &context{root: root}, nil
}

// Sanitize validates that the path p points to a resource inside the context
// and sanitizes it. If the path cannot be sanitized or points outside of the
// context, an error is returned.
// TODO(stevvooe): This method name needs to be changed.
func (c *context) Sanitize(p string) (string, error) {
	return sanitize(c.root, p)
}

// Path returns the full path of p within the context. If the path escapes the
// context root, an error is returned.
func (c *context) Path(p string) (string, error) {
	p = filepath.Join(c.root, p)
	if !strings.HasPrefix(p, c.root) {
		return "", fmt.Errorf("invalid context path")
	}

	return p, nil
}

// Digest returns the digest of the file at path p, relative to the root.
func (c *context) Digest(p string) (digest.Digest, error) {
	return digestPath(filepath.Join(c.root, p))
}

// Resource returns the resource as path p, populating the entry with info
// from fi. The path p should be the path of the resource in the context,
// typically obtained from a call to Sanitize or the value of Resource.Path().
// If fi is nil, os.Lstat will be used resolve it.
func (c *context) Resource(p string, fi os.FileInfo) (Resource, error) {
	fp, err := c.Path(p)
	if err != nil {
		return nil, err
	}

	log.Println("resource", p, fp)
	if fi == nil {
		fi, err = os.Lstat(fp)
		if err != nil {
			return nil, err
		}
	}

	base, err := newBaseResource(p, fi)
	if err != nil {
		return nil, err
	}

	base.xattrs, err = c.resolveXAttrs(fp, fi, base)
	if err != nil {
		return nil, err
	}

	// TODO(stevvooe): Handle windows alternate data streams.

	if fi.Mode().IsRegular() {
		dgst, err := digestPath(fp)
		if err != nil {
			return nil, err
		}

		return newRegularFile(p, fi, base, dgst)
	}

	if fi.Mode().IsDir() {
		return newDirectory(p, fi, base)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		// We handle relative links vs absolute links by including a
		// beginning slash for absolute links. Effectively, the bundle's
		// root is treated as the absolute link anchor.
		target, err := os.Readlink(fp)
		if err != nil {
			return nil, err
		}

		// TODO(stevvooe): Re-do this when we have fully vetted path
		// management methods on context.
		if filepath.IsAbs(target) {

			// TODO(stevvooe): This path needs to be "sanitized" (or contained
			// or whatever we end up calling it).

			// When path is absolute, we make it relative to the bundle root.
			target, err = filepath.Rel(c.root, target)
			if err != nil {
				return nil, err
			}

			// now make the target absolute, since we want to maintain that.
			target = filepath.Join("/", target)
		} else {
			// make sure the target is contained in the root.
			real := filepath.Join(fp, target)
			if !strings.HasPrefix(real, c.root) {
				return nil, fmt.Errorf("link refers to file outside of root: %q -> %q, real = %q", fp, target, real)
			}
		}

		return newSymLink(p, fi, base, target)
	}

	if fi.Mode()&os.ModeNamedPipe != 0 {
		return newNamedPipe(p, fi, base)
	}

	// TODO(stevvooe): Implement support for devices and make a note about
	// socket support. The below was lifted from the refactored BuildManifest.

	// if fi.Mode()&os.ModeDevice != 0 {
	// 	// character and block devices merely need to recover the
	// 	// major/minor device number.
	// 	entry.Major = uint32(major(uint(sysStat.Rdev)))
	// 	entry.Minor = uint32(minor(uint(sysStat.Rdev)))
	// }

	// if fi.Mode()&os.ModeSocket != 0 {
	// 	return nil // sockets are skipped, no point
	// }

	log.Printf("%q (%v) is not supported", fp, fi.Mode())
	return nil, ErrNotFound
}

// Verify the resource in the context. An error will be returned a discrepancy
// is found.
func (c *context) Verify(resource Resource) error {
	panic("not implemented")
}

// Apply the resource to the contexts. An error will be returned in the
// operation fails. Depending on the resource type, the resource may be
// created. For resource that cannot be resolved, an error will be returned.
func (c *context) Apply(resource Resource) error {
	panic("not implemented")
}

// Walk provides a convenience function to call filepath.Walk correctly for
// the context. The behavior is otherwise identical.
func (c *context) Walk(fn filepath.WalkFunc) error {
	return filepath.Walk(c.root, fn)
}

// resolveXAttrs attempts to resolve the extended attributes for the resource
// at the path fp, which is the full path to the resource. If the resource
// cannot have xattrs, nil will be returned.
func (c *context) resolveXAttrs(fp string, fi os.FileInfo, base *resource) (map[string][]byte, error) {
	// Restricts xattrs to only the below file types. This allowance of
	// symlinks is slightly questionable, since their support is spotty on
	// most file systems and xattrs are generally stored in the inode. This
	// may belong elsewhere.
	if !(fi.Mode().IsRegular() || fi.Mode().IsDir()) {
		return nil, nil
	}

	// TODO(stevvooe): This is very preliminary support for xattrs. We
	// still need to ensure that links aren't being followed.
	xattrs, err := Listxattr(fp)
	if err != nil {
		log.Println("error listing xattrs ", fp)
		return nil, err
	}

	sort.Strings(xattrs)
	m := make(map[string][]byte, len(xattrs))

	for _, attr := range xattrs {
		value, err := Getxattr(fp, attr)
		if err != nil {
			log.Printf("error getting xattrs: %v %q %v %v", fp, attr, xattrs, len(xattrs))
			return nil, err
		}

		// NOTE(stevvooe): This append/copy tricky relies on unique
		// xattrs. Break this out into an alloc/copy if xattrs are no
		// longer unique.
		m[attr] = append(base.xattrs[attr], value...)
	}

	return m, nil
}

// sanitize and clean the path relative to root.
func sanitize(root, p string) (string, error) {
	sanitized, err := filepath.Rel(root, p)
	if err != nil {
		return "", err
	}

	return filepath.Join("/", filepath.Clean(sanitized)), nil
}
