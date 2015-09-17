package diffutil

import (
	"github.com/Sirupsen/logrus"
	"github.com/stevvooe/continuity"
)

type ResourceUpdate struct {
	Original continuity.Resource
	Updated  continuity.Resource
}

type ManifestDifference struct {
	Additions []continuity.Resource
	Deletions []continuity.Resource
	Updates   []ResourceUpdate
}

// DiffManifest compares two manifests and returns the list
// of adds updates and deletes
func DiffManifest(m1, m2 *continuity.Manifest) ManifestDifference {
	i1 := 0
	i2 := 0
	var d ManifestDifference

	for i1 < len(m1.Resources) && i2 < len(m2.Resources) {
		p1 := m1.Resources[i1].Path()
		p2 := m2.Resources[i2].Path()
		switch {
		case p1 < p2:
			logrus.Debugf("Deletion %s", p1)
			d.Deletions = append(d.Deletions, m1.Resources[i1])
			i1++
		case p1 == p2:
			logrus.Debugf("Comparing %s to %s", p1, p2)
			if !Compare(m1.Resources[i1], m2.Resources[i2]) {
				d.Updates = append(d.Updates, ResourceUpdate{
					Original: m1.Resources[i1],
					Updated:  m2.Resources[i2],
				})
			}
			i1++
			i2++
		case p1 > p2:
			logrus.Debugf("Addition %s", p2)
			d.Additions = append(d.Additions, m2.Resources[i2])
			i2++
		}
	}

	for i1 < len(m1.Resources) {
		d.Deletions = append(d.Deletions, m1.Resources[i1])
		i1++

	}
	for i2 < len(m2.Resources) {
		d.Additions = append(d.Additions, m1.Resources[i2])
		i2++
	}

	return d
}

func Compare(r1, r2 continuity.Resource) bool {
	if r1.Path() != r2.Path() {
		return false
	}
	if r1.Mode() != r2.Mode() {
		return false
	}
	if r1.UID() != r2.UID() {
		return false
	}
	if r1.GID() != r2.GID() {
		return false
	}

	switch t1 := r1.(type) {
	case continuity.RegularFile:
		t2, ok := r2.(continuity.RegularFile)
		if !ok {
			return false
		}
		return compareRegularFile(t1, t2)
	case continuity.Directory:
		t2, ok := r2.(continuity.Directory)
		if !ok {
			return false
		}
		return compareDirectory(t1, t2)
	case continuity.SymLink:
		t2, ok := r2.(continuity.SymLink)
		if !ok {
			return false
		}
		return compareSymLink(t1, t2)
	case continuity.NamedPipe:
		t2, ok := r2.(continuity.NamedPipe)
		if !ok {
			return false
		}
		return compareNamedPipe(t1, t2)
	case continuity.Device:
		t2, ok := r2.(continuity.Device)
		if !ok {
			return false
		}
		return compareDevice(t1, t2)
	default:
		// TODO(dmcgowan): Should this panic?
		return r1 == r2
	}
}

func compareRegularFile(r1, r2 continuity.RegularFile) bool {
	if r1.Size() != r2.Size() {
		return false
	}
	p1 := r1.Paths()
	p2 := r2.Paths()
	if len(p1) != len(p2) {
		return false
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			return false
		}
	}
	d1 := r1.Digests()
	d2 := r2.Digests()
	if len(d1) != len(d2) {
		return false
	}
	for i := range d1 {
		if d1[i] != d2[i] {
			return false
		}
	}

	// TODO(dmcgowan): Compare Xattrs
	return true
}

func compareSymLink(r1, r2 continuity.SymLink) bool {
	return r1.Target() == r2.Target()
}

func compareDirectory(r1, r2 continuity.Directory) bool {
	// TODO(dmcgowan): Compare Xattrs
	return true
}

func compareNamedPipe(r1, r2 continuity.NamedPipe) bool {
	return true
}

func compareDevice(r1, r2 continuity.Device) bool {
	return r1.Major() == r2.Major() && r1.Minor() == r2.Minor()
}
