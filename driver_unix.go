package continuity

import (
	"fmt"
	"sort"
)

// Getxattr returns all of the extended attributes for the file at path p.
func (d *driver) Getxattr(p string) (map[string][]byte, error) {
	xattrs, err := Listxattr(p)
	if err != nil {
		return nil, fmt.Errorf("listing %s xattrs: %v", p, err)
	}

	sort.Strings(xattrs)
	m := make(map[string][]byte, len(xattrs))

	for _, attr := range xattrs {
		value, err := Getxattr(p, attr)
		if err != nil {
			return nil, fmt.Errorf("getting %q xattr on %s: %v", p, attr, err)
		}

		// NOTE(stevvooe): This append/copy tricky relies on unique
		// xattrs. Break this out into an alloc/copy if xattrs are no
		// longer unique.
		m[attr] = append(m[attr], value...)
	}

	return m, nil
}

// Setxattr sets all of the extended attributes on file at path, following
// any symbolic links, if necessary. All attributes on the target are
// replaced by the values from attr. If the operation fails to set any
// attribute, those already applied will not be rolled back.
func (d *driver) Setxattr(path string, attr map[string][]byte) error {
	panic("not implemented")
}
