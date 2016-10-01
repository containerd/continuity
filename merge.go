package continuity

import (
	"sort"
	"strings"
)

// MergeManifests merges a manifest onto another manifest. Only
// the diff manifest should contain whiteout information.
func MergeManifests(manifest, diff *Manifest) *Manifest {
	r1 := manifest.Resources
	sort.Sort(ByPath(r1))
	r2 := diff.Resources
	sort.Sort(ByPath(r2))
	return mergeResources(r1, r2)
}

// mergeResources merges two ordered set of resources into manifest
// using the provided whiteout and comparison function.
// TODO(dmcgowan): Handle handlinks
func mergeResources(r1, r2 []Resource) *Manifest {
	result := make([]Resource, 0, len(r1))

	i1 := 0
	i2 := 0

	for i1 < len(r1) && i2 < len(r2) {
		p1 := r1[i1].Path()
		p2 := r2[i2].Path()

		switch {
		case p1 < p2:
			result = append(result, r1[i1])
			i1++
		case p1 == p2:
			// p1 will be replaced by p2
			i1++
			fallthrough
		default:
			var skipPath string
			switch resource := r2[i2].(type) {
			case Whiteout:
				skipPath = asDir(resource.Path())
			case Directory:
				if resource.IsOpaque() {
					skipPath = asDir(resource.Path())
					result = append(result, Resource(removeOpaqueness(resource)))
				} else {
					result = append(result, r2[i2])
				}
			default:
				// Not a directory, skip any files under path (replaces directory)
				skipPath = asDir(resource.Path())
				result = append(result, r2[i2])
			}
			if skipPath != "" {
				for i1 < len(r1) && strings.HasPrefix(r1[i1].Path(), skipPath) {
					// Ignore resource in opaque or deleted directory
					i1++
				}
			}
			i2++
		}
	}

	for i1 < len(r1) {
		result = append(result, r1[i1])
		i1++
	}
	for i2 < len(r2) {
		switch resource := r2[i2].(type) {
		case Whiteout:
			// Ignore, no more files to whiteout
		case Directory:
			if resource.IsOpaque() {
				result = append(result, Resource(removeOpaqueness(resource)))
			} else {
				result = append(result, r2[i2])
			}
		default:
			result = append(result, r2[i2])
		}
		i2++
	}

	return &Manifest{
		Resources: result,
	}
}

func asDir(name string) string {
	if name == "" || name[len(name)-1] != '/' {
		return name + "/"
	}
	return name
}
