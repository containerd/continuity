package continuity

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/distribution/digest"
)

var (
	ErrNotFound     = fmt.Errorf("not found")
	ErrNotSupported = fmt.Errorf("not supported")
)

// Context represents a file system context for accessing resources. The
// responsibility of the context is to convert system specific resources to
// generic Resource objects. Most of this is safe path manipulation, as well
// as extraction of resource details.
type Context interface {
	Apply(Resource) error
	Verify(Resource) error
	Resource(string, os.FileInfo) (Resource, error)
	Walk(filepath.WalkFunc) error
}

// context represents a file system context for accessing resources.
// Generally, all path qualified access and system considerations should land
// here.
type context struct {
	driver Driver
	root   string
}

// NewContext returns a Context associated with root. The default driver will
// be used, as returned by NewDriver.
func NewContext(root string) (Context, error) {
	// normalize to absolute path
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}

	driver, err := NewSystemDriver()
	if err != nil {
		return nil, err
	}

	// Check the root directory. Need to be a little careful here. We are
	// allowing a link for now, but this may have odd behavior when
	// canonicalizing paths. As long as all files are opened through the link
	// path, this should be okay.
	fi, err := driver.Stat(root)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, &os.PathError{Op: "NewContext", Path: root, Err: os.ErrInvalid}
	}

	return &context{root: root, driver: driver}, nil
}

// Resource returns the resource as path p, populating the entry with info
// from fi. The path p should be the path of the resource in the context,
// typically obtained through Walk or from the value of Resource.Path(). If fi
// is nil, it will be resolved.
func (c *context) Resource(p string, fi os.FileInfo) (Resource, error) {
	fp, err := c.fullpath(p)
	if err != nil {
		return nil, err
	}

	if fi == nil {
		fi, err = c.driver.Lstat(fp)
		if err != nil {
			return nil, err
		}
	}

	base, err := newBaseResource(p, fi)
	if err != nil {
		return nil, err
	}

	base.xattrs, err = c.resolveXAttrs(fp, fi, base)
	if err == ErrNotSupported {
		log.Printf("resolving xattrs on %s not supported", fp)
	} else if err != nil {
		return nil, err
	}

	// TODO(stevvooe): Handle windows alternate data streams.

	if fi.Mode().IsRegular() {
		dgst, err := c.digest(p)
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
		target, err := c.driver.Readlink(fp)
		if err != nil {
			return nil, err
		}

		if filepath.IsAbs(target) {
			// contain the absolute path to the context root.
			target, err = c.contain(p)
			if err != nil {
				return nil, err
			}
		} else {
			// make sure the target is contained in the root by evaluating the
			// link and checking the prefix.
			real := filepath.Join(fp, target)
			if !strings.HasPrefix(real, c.root) {
				return nil, fmt.Errorf("uncontained symlink: %q -> %q, real = %q", fp, target, real)
			}
		}

		return newSymLink(p, fi, base, target)
	}

	if fi.Mode()&os.ModeNamedPipe != 0 {
		return newNamedPipe(p, fi, base)
	}

	if fi.Mode()&os.ModeDevice != 0 {
		deviceDriver, ok := c.driver.(DeviceInfoDriver)
		if !ok {
			log.Printf("device extraction not supported %s", fp)
			return nil, ErrNotSupported
		}

		// character and block devices merely need to recover the
		// major/minor device number.
		major, minor, err := deviceDriver.DeviceInfo(fi)
		if err != nil {
			return nil, err
		}

		return newDevice(p, fi, base, major, minor)
	}

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
// the context. Otherwise identical to filepath.Walk, the path argument is
// corrected to be contained within the context.
func (c *context) Walk(fn filepath.WalkFunc) error {
	return filepath.Walk(c.root, func(p string, fi os.FileInfo, err error) error {
		contained, err := c.contain(p)
		return fn(contained, fi, err)
	})
}

// fullpath returns the system path for the resource, joined with the context
// root. The path p must be a part of the context.
func (c *context) fullpath(p string) (string, error) {
	p = filepath.Join(c.root, p)
	if !strings.HasPrefix(p, c.root) {
		return "", fmt.Errorf("invalid context path")
	}

	return p, nil
}

// contain cleans and santizes the filesystem path p to be an absolute path,
// effectively relative to the context root.
func (c *context) contain(p string) (string, error) {
	sanitized, err := filepath.Rel(c.root, p)
	if err != nil {
		return "", err
	}

	// ZOMBIES(stevvooe): In certain cases, we may want to remap these to a
	// "containment error", so the caller can decide what to do.
	return filepath.Join("/", filepath.Clean(sanitized)), nil
}

// digest returns the digest of the file at path p, relative to the root.
func (c *context) digest(p string) (digest.Digest, error) {
	return digestPath(c.driver, filepath.Join(c.root, p))
}

// resolveXAttrs attempts to resolve the extended attributes for the resource
// at the path fp, which is the full path to the resource. If the resource
// cannot have xattrs, nil will be returned.
func (c *context) resolveXAttrs(fp string, fi os.FileInfo, base *resource) (map[string][]byte, error) {
	if fi.Mode().IsRegular() || fi.Mode().IsDir() {
		xattrDriver, ok := c.driver.(XAttrDriver)
		if !ok {
			log.Println("xattr extraction not supported")
			return nil, ErrNotSupported
		}

		return xattrDriver.Getxattr(fp)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		lxattrDriver, ok := c.driver.(LXAttrDriver)
		if !ok {
			log.Println("xattr extraction for symlinks not supported")
			return nil, ErrNotSupported
		}

		return lxattrDriver.LGetxattr(fp)
	}

	return nil, nil
}
