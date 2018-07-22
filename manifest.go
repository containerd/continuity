package continuity

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"sort"

	pb "github.com/containerd/continuity/proto"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

const (
	// MediaTypeManifestV0Protobuf is the media type for manifest formatted as protobuf.
	// The format is unstable during v0.
	MediaTypeManifestV0Protobuf = "application/vnd.continuity.manifest.v0+pb"
	// MediaTypeManifestV0JSON is the media type for manifest formatted as JSON.
	// JSON is marshalled from protobuf using jsonpb.Marshaler
	// ({EnumAsInts = false, EmitDefaults = false, OrigName = false})
	// The format is unstable during v0.
	MediaTypeManifestV0JSON = "application/vnd.continuity.manifest.v0+json"
)

// Manifest provides the contents of a manifest. Users of this struct should
// not typically modify any fields directly.
type Manifest struct {
	// Resources specifies all the resources for a manifest in order by path.
	Resources []Resource
}

func manifestFromProto(bm *pb.Manifest) (*Manifest, error) {
	var m Manifest
	for _, b := range bm.Resource {
		r, err := fromProto(b)
		if err != nil {
			return nil, err
		}

		m.Resources = append(m.Resources, r)
	}
	return &m, nil
}

func Unmarshal(p []byte) (*Manifest, error) {
	var bm pb.Manifest
	if err := proto.Unmarshal(p, &bm); err != nil {
		return nil, err
	}
	return manifestFromProto(&bm)
}

func UnmarshalJSON(p []byte) (*Manifest, error) {
	var bm pb.Manifest
	if err := jsonpb.Unmarshal(bytes.NewReader(p), &bm); err != nil {
		return nil, err
	}
	return manifestFromProto(&bm)
}

func manifestToProto(m *Manifest) *pb.Manifest {
	var bm pb.Manifest
	for _, resource := range m.Resources {
		bm.Resource = append(bm.Resource, toProto(resource))
	}
	return &bm
}

func Marshal(m *Manifest) ([]byte, error) {
	return proto.Marshal(manifestToProto(m))
}

func MarshalText(w io.Writer, m *Manifest) error {
	return proto.MarshalText(w, manifestToProto(m))
}

func MarshalJSON(m *Manifest) ([]byte, error) {
	var b bytes.Buffer
	marshaler := &jsonpb.Marshaler{}
	if err := marshaler.Marshal(&b, manifestToProto(m)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// BuildManifest creates the manifest for the given context
func BuildManifest(ctx Context) (*Manifest, error) {
	resourcesByPath := map[string]Resource{}
	hardlinks := newHardlinkManager()

	if err := ctx.Walk(func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking %s: %v", p, err)
		}

		if p == string(os.PathSeparator) {
			// skip root
			return nil
		}

		resource, err := ctx.Resource(p, fi)
		if err != nil {
			if err == ErrNotFound {
				return nil
			}
			log.Printf("error getting resource %q: %v", p, err)
			return err
		}

		// add to the hardlink manager
		if err := hardlinks.Add(fi, resource); err == nil {
			// Resource has been accepted by hardlink manager so we don't add
			// it to the resourcesByPath until we merge at the end.
			return nil
		} else if err != errNotAHardLink {
			// handle any other case where we have a proper error.
			return fmt.Errorf("adding hardlink %s: %v", p, err)
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

	var resources []Resource
	for _, resource := range resourcesByPath {
		resources = append(resources, resource)
	}

	sort.Stable(ByPath(resources))

	return &Manifest{
		Resources: resources,
	}, nil
}

// VerifyManifest verifies all the resources in a manifest
// against files from the given context.
func VerifyManifest(ctx Context, manifest *Manifest) error {
	for _, resource := range manifest.Resources {
		if err := ctx.Verify(resource); err != nil {
			return err
		}
	}

	return nil
}

// ApplyManifest applies on the resources in a manifest to
// the given context.
func ApplyManifest(ctx Context, manifest *Manifest) error {
	for _, resource := range manifest.Resources {
		if err := ctx.Apply(resource); err != nil {
			return err
		}
	}

	return nil
}
