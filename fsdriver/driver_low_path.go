package fsdriver

import (
	"errors"
	"path"
	"strings"
)

func (d *lowDriver) Join(pathName ...string) string {
	return path.Join(pathName...)
}

func (d *lowDriver) IsAbs(pathName string) bool {
	return path.IsAbs(pathName)
}

func (d *lowDriver) Rel(base, target string) (string, error) {
	// This is mostly copied from the Go filepath.Rel function since the
	// path package does not have Rel.
	baseClean, targetClean := d.Clean(base), d.Clean(target)

	// If one path is relative, but the other is absolute, we would need to
	// know the current directory figure out where the path actually is.
	if d.IsAbs(baseClean) != d.IsAbs(targetClean) {
		return "", errors.New("Rel: can't make " + target + " relative to " + base)
	}

	// Position base[b0:bi] and targ[t0:ti] at the first differing elements.
	bl := len(baseClean)
	tl := len(targetClean)
	var b0, bi, t0, ti int
	for {
		for bi < bl && baseClean[bi] != '/' {
			bi++
		}
		for ti < tl && targetClean[ti] != '/' {
			ti++
		}
		if targetClean[t0:ti] != baseClean[b0:bi] {
			break
		}
		if bi < bl {
			bi++
		}
		if ti < tl {
			ti++
		}
		b0 = bi
		t0 = ti
	}
	if baseClean[b0:bi] == ".." {
		return "", errors.New("Rel: can't make " + target + " relative to " + base)
	}

	if b0 != bl {
		// Base elements left. Must go up before going down.
		seps := strings.Count(baseClean[b0:bl], "/")
		size := 2 + seps*3
		if tl != t0 {
			size += 1 + tl - t0
		}
		buf := make([]byte, size)
		n := copy(buf, "..")
		for i := 0; i < seps; i++ {
			buf[n] = '/'
			copy(buf[n+1:], "..")
			n += 3
		}
		if t0 != tl {
			buf[n] = '/'
			copy(buf[n+1:], targetClean[t0:])
		}
		return string(buf), nil
	}
	return targetClean[t0:], nil
}

func (d *lowDriver) Base(pathName string) string {
	return path.Base(pathName)
}

func (d *lowDriver) Dir(pathName string) string {
	return path.Dir(pathName)
}

func (d *lowDriver) Clean(pathName string) string {
	return path.Clean(pathName)
}

func (d *lowDriver) Split(pathName string) (dir, file string) {
	return path.Split(pathName)
}

func (d *lowDriver) Separator() byte {
	return '/'
}

func (d *lowDriver) NormalizePath(pathName string) string {
	return pathName
}
