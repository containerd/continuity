package continuity

import (
	"testing"

	"github.com/docker/distribution/digest"
)

func randomDigest(size int) digest.Digest {
	d := make([]byte, size)
	randomBytes(d)
	return digest.FromBytes(d)
}

func TestMerge(t *testing.T) {
	layer1 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   4085,
			digest: randomDigest(4085),
			path:   "a/f1",
			mode:   0600,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "a/f2",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/hidden",
			mode:   0600,
		},
		{
			kind: rdirectory,
			path: "c",
			mode: 0755,
		},
		{
			path: "c/f1",
			mode: 0600,
		},
	}
	layer2 := []dresource{
		{
			kind: rdirectory,
			path: "a",
			mode: 0755,
		},
		{
			kind:   rfile,
			size:   1022,
			digest: randomDigest(1022),
			path:   "a/f2",
			mode:   0644,
		},
		{
			kind:   rfile,
			size:   234,
			digest: randomDigest(234),
			path:   "a/f3",
			mode:   0600,
		},
		{
			kind:   rdirectory,
			path:   "b",
			mode:   0755,
			opaque: true,
		},
		{
			kind:   rfile,
			size:   1023,
			digest: randomDigest(1023),
			path:   "b/nothidden",
			mode:   0600,
		},
		{
			kind: rwhiteout,
			path: "c",
		},
	}
	result := []dresource{
		layer2[0],
		layer1[1],
		layer2[1],
		layer2[2],
		{
			kind: rdirectory,
			path: "b",
			mode: 0755,
		},
		layer2[4],
	}

	checkMerge(t, layer1, layer2, result)
}

func checkMerge(t *testing.T, layer1, layer2, result []dresource) {
	r1, err := expectedResourceList(layer1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := expectedResourceList(layer2)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := expectedResourceList(result)
	if err != nil {
		t.Fatal(err)
	}

	mm := MergeManifests(&Manifest{Resources: r1}, &Manifest{Resources: r2})

	diff := diffResourceList(expected, mm.Resources)
	if diff.HasDiff() {
		t.Log("Resource list difference")
		for _, a := range diff.Additions {
			t.Logf("Unexpected resource: %#v", a)
		}
		for _, d := range diff.Deletions {
			t.Logf("Missing resource: %#v", d)
		}
		for _, u := range diff.Updates {
			t.Logf("Changed resource:\n\tExpected: %#v\n\tActual:   %#v", u.Original, u.Updated)
		}

		t.FailNow()
	}
}
