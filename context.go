package continuity

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

// SymlinkPath is intended to give the symlink target value
// in a root context. Target and linkname are absolute paths
// not under the given root.
type SymlinkPath func(root, linkname, target string) (string, error)

// context represents a file system context for accessing resources.
// Generally, all path qualified access and system considerations should land
// here.
type context struct {
	driver  Driver
	root    string
	symPath SymlinkPath
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

	return &context{
		root:    root,
		driver:  driver,
		symPath: AbsoluteSymlinkPath,
	}, nil
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

	// TODO(stevvooe): This need to be resolved for the container's root,
	// where here we are really getting the host OS's value. We need to allow
	// this be passed in and fixed up to make these uid/gid mappings portable.
	// Either this can be part of the driver or we can achieve it through some
	// other mechanism.
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		// TODO(stevvooe): This may not be a hard error for all platforms. We
		// may want to move this to the driver.
		return nil, fmt.Errorf("unable to resolve syscall.Stat_t from (os.FileInfo).Sys(): %#v", fi)
	}

	base, err := newBaseResource(p, fi.Mode(), fmt.Sprint(sys.Uid), fmt.Sprint(sys.Gid))
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

		return newRegularFile(base, base.paths, fi.Size(), dgst)
	}

	if fi.Mode().IsDir() {
		return newDirectory(base)
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
			target, err = c.contain(target)
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

		return newSymLink(base, target)
	}

	if fi.Mode()&os.ModeNamedPipe != 0 {
		return newNamedPipe(base)
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

		return newDevice(base, major, minor)
	}

	log.Printf("%q (%v) is not supported", fp, fi.Mode())
	return nil, ErrNotFound
}

// Verify the resource in the context. An error will be returned a discrepancy
// is found.
func (c *context) Verify(resource Resource) error {
	target, err := c.Resource(resource.Path(), nil)
	if err != nil {
		return err
	}

	if target.Path() != resource.Path() {
		return fmt.Errorf("resource paths do not match: %q != %q", target.Path(), resource.Path())
	}

	if target.Mode() != resource.Mode() {
		return fmt.Errorf("resource %q has incorrect mode: %v != %v", target.Path(), target.Mode(), resource.Mode())
	}

	if target.UID() != resource.UID() {
		return fmt.Errorf("unexpected uid for %q: %v != %v", target.Path(), target.UID(), resource.GID())
	}

	if target.GID() != resource.GID() {
		return fmt.Errorf("unexpected gid for %q: %v != %v", target.Path(), target.GID(), target.GID())
	}

	if xattrer, ok := resource.(XAttrer); ok {
		txattrer, tok := target.(XAttrer)
		if !tok {
			return fmt.Errorf("resource %q has xattrs but target does not support them", resource.Path())
		}

		// For xattrs, only ensure that we have those defined in the resource
		// and their values match. We can ignore other xattrs. In other words,
		// we only verify that target has the subset defined by resource.
		txattrs := txattrer.XAttrs()
		for attr, value := range xattrer.XAttrs() {
			tvalue, ok := txattrs[attr]
			if !ok {
				return fmt.Errorf("resource %q target missing xattr %q", resource.Path(), attr)
			}

			if !bytes.Equal(value, tvalue) {
				return fmt.Errorf("xattr %q value differs for resource %q", attr, resource.Path())
			}
		}
	}

	switch r := resource.(type) {
	case RegularFile:
		// TODO(stevvooe): We need to grab a target for each path, since a
		// regular file may be a hardlink. Effectively, we must use t.Paths()
		// somewhere in here.
		if len(r.Paths()) > 1 {
			panic("not implemented")
		}

		// TODO(stevvooe): Another reason to use a record-based approach. We
		// have to do another type switch to get this to work. This could be
		// fixed with an Equal function, but let's study this a little more to
		// be sure.
		t, ok := target.(RegularFile)
		if !ok {
			return fmt.Errorf("resource %q target not a regular file", r.Path())
		}

		if t.Size() != r.Size() {
			return fmt.Errorf("resource %q target has incorrect size: %v != %v", t.Path(), t.Size(), r.Size())
		}

		// TODO(stevvooe): This may need to get a little more sophisticated
		// for digest comparison. We may want to actually calculate the
		// provided digests, rather than the implementations having an
		// overlap.
		if !digestsMatch(t.Digests(), r.Digests()) {
			return fmt.Errorf("digests for resource %q do not match: %v != %v", t.Path(), t.Digests(), r.Digests())
		}
	case Directory:
		// nothing to be done here.
	case SymLink:
		t, ok := target.(SymLink)
		if !ok {
			return fmt.Errorf("resource %q target not a symlink: %v", t)
		}

		if t.Target() != r.Target() {
			return fmt.Errorf("resource %q target has mismatched target: %q != %q", t.Target(), r.Target())
		}
	default:
		return fmt.Errorf("cannot verify resource: %v", resource)
	}

	return nil
}

// Apply the resource to the contexts. An error will be returned if the
// operation fails. Depending on the resource type, the resource may be
// created. For resource that cannot be resolved, an error will be returned.
func (c *context) Apply(resource Resource) error {
	fp, err := c.fullpath(resource.Path())
	if err != nil {
		return err
	}

	var chmod = true
	var exists bool
	if _, err := c.driver.Lstat(fp); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		exists = true
	}

	switch r := resource.(type) {
	case RegularFile:
		if !exists {
			return fmt.Errorf("file does not exist %q", resource.Path())
		}

		// TODO(dmcgowan): Verify size and digest

		for _, path := range r.Paths() {
			if path != resource.Path() {
				lp, err := c.fullpath(path)
				if err != nil {
					return err
				}

				if _, fi := c.driver.Lstat(lp); fi == nil {
					c.driver.Remove(lp)
				}
				if err := c.driver.Link(fp, lp); err != nil {
					return err
				}
			}
		}

	case Directory:
		if !exists {
			if err := c.driver.Mkdir(fp, resource.Mode()); err != nil {
				return err
			}
		}
	case SymLink:
		target, err := c.resolveSymlink(r)
		if err != nil {
			return err
		}
		if exists {
			currentPath, err := c.driver.Readlink(fp)
			if err != nil {
				return err
			}
			if currentPath != target {
				if err := c.driver.Remove(fp); err != nil {
					return err
				}
				exists = false
			}
		}
		if !exists {
			if err := c.driver.Symlink(target, fp); err != nil {
				return err
			}
			// Not supported on linux, skip chmod on links
			//if err := c.driver.Lchmod(fp, resource.Mode()); err != nil {
			//	return err
			//}
		}
		chmod = false
	}

	// Update filemode if file was not created
	if chmod && exists {
		if err := c.driver.Lchmod(fp, resource.Mode()); err != nil {
			return err
		}
	}

	if err := c.driver.Lchown(fp, resource.UID(), resource.GID()); err != nil {
		return err
	}

	if xattrer, ok := resource.(XAttrer); ok {
		// For xattrs, only ensure that we have those defined in the resource
		// and their values are set. We can ignore other xattrs. In other words,
		// we only set xattres defined by resource but never remove.

		if _, ok := resource.(SymLink); ok {
			lxattrDriver, ok := c.driver.(LXAttrDriver)
			if !ok {
				return fmt.Errorf("unsupported symlink xattr for resource %q", resource.Path())
			}
			if err := lxattrDriver.LSetxattr(fp, xattrer.XAttrs()); err != nil {
				return err
			}
		} else {
			xattrDriver, ok := c.driver.(XAttrDriver)
			if !ok {
				return fmt.Errorf("unsupported xattr for resource %q", resource.Path())
			}
			if err := xattrDriver.Setxattr(fp, xattrer.XAttrs()); err != nil {
				return err
			}
		}
	}

	return nil
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

func (c *context) resolveSymlink(l SymLink) (string, error) {
	target := l.Target()
	if filepath.IsAbs(target) {
		return c.symPath(c.root, l.Path(), target)
	}
	return target, nil
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

// AbsoluteSymlinkPath turns the symlink target into absolute paths from
// the given root.
func AbsoluteSymlinkPath(root, linkname, target string) (string, error) {
	return filepath.Join(root, target), nil
}

// RelativeSymlinkPath turns the symlink target into a relative path
// from the given linkname.
func RelativeSymlinkPath(root, linkname, target string) (string, error) {
	return filepath.Rel(filepath.Join(root, linkname), target)
}

// ChrootSymlinkPath uses the given absolute target intended for a
// chroot environment using the given root.
func ChrootSymlinkPath(root, linkname, target string) (string, error) {
	return target, nil
}
