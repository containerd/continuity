// Code generated by protoc-gen-go.
// source: manifest.proto
// DO NOT EDIT!

/*
Package proto is a generated protocol buffer package.

It is generated from these files:
	manifest.proto

It has these top-level messages:
	Manifest
	Resource
	XAttr
	ADSEntry
*/
package proto

import proto1 "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto1.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto1.ProtoPackageIsVersion2 // please upgrade the proto package

// Manifest specifies the entries in a container bundle, keyed and sorted by
// path.
type Manifest struct {
	Resource []*Resource `protobuf:"bytes,1,rep,name=resource" json:"resource,omitempty"`
}

func (m *Manifest) Reset()                    { *m = Manifest{} }
func (m *Manifest) String() string            { return proto1.CompactTextString(m) }
func (*Manifest) ProtoMessage()               {}
func (*Manifest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

func (m *Manifest) GetResource() []*Resource {
	if m != nil {
		return m.Resource
	}
	return nil
}

type Resource struct {
	// Path specifies the path from the bundle root. If more than one
	// path is present, the entry may represent a hardlink, rather than using
	// a link target.
	// A path must be relative to the bundle root.
	// TODO(AkihiroSuda): use '/' seperator regardless of the operating system used for building the manifest?
	Path []string `protobuf:"bytes,1,rep,name=path" json:"path,omitempty"`
	// Uid specifies the user id for the resource.
	Uid int64 `protobuf:"varint,2,opt,name=uid" json:"uid,omitempty"`
	// Gid specifies the group id for the resource.
	Gid int64 `protobuf:"varint,3,opt,name=gid" json:"gid,omitempty"`
	// user and group are not currently used but their field numbers have been
	// reserved for future use. As such, they are marked as deprecated.
	User  string `protobuf:"bytes,4,opt,name=user" json:"user,omitempty"`
	Group string `protobuf:"bytes,5,opt,name=group" json:"group,omitempty"`
	// Mode defines the file mode and permissions. We've used the same
	// bit-packing from Go's os package,
	// http://golang.org/pkg/os/#FileMode, since they've done the work of
	// creating a cross-platform layout.
	Mode uint32 `protobuf:"varint,6,opt,name=mode" json:"mode,omitempty"`
	// Size specifies the size in bytes of the resource. This is only valid
	// for regular files.
	Size uint64 `protobuf:"varint,7,opt,name=size" json:"size,omitempty"`
	// Digest specifies the content digest of the target file. Only valid for
	// regular files. The strings are formatted in OCI style, i.e. <alg>:<encoded>.
	// For detailed information about the format, please refer to OCI Image Spec:
	// https://github.com/opencontainers/image-spec/blob/master/descriptor.md#digests-and-verification
	// The digests are sorted in lexical order and implementations may choose
	// which algorithms they prefer.
	Digest []string `protobuf:"bytes,8,rep,name=digest" json:"digest,omitempty"`
	// Target defines the target of a hard or soft link. Absolute links start
	// with a slash and specify the resource relative to the bundle root.
	// Relative links do not start with a slash and are relative to the
	// resource path.
	Target string `protobuf:"bytes,9,opt,name=target" json:"target,omitempty"`
	// Major specifies the major device number for charactor and block devices.
	Major uint64 `protobuf:"varint,10,opt,name=major" json:"major,omitempty"`
	// Minor specifies the minor device number for charactor and block devices.
	Minor uint64 `protobuf:"varint,11,opt,name=minor" json:"minor,omitempty"`
	// Xattr provides storage for extended attributes for the target resource.
	Xattr []*XAttr `protobuf:"bytes,12,rep,name=xattr" json:"xattr,omitempty"`
	// Ads stores one or more alternate data streams for the target resource.
	Ads []*ADSEntry `protobuf:"bytes,13,rep,name=ads" json:"ads,omitempty"`
}

func (m *Resource) Reset()                    { *m = Resource{} }
func (m *Resource) String() string            { return proto1.CompactTextString(m) }
func (*Resource) ProtoMessage()               {}
func (*Resource) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *Resource) GetXattr() []*XAttr {
	if m != nil {
		return m.Xattr
	}
	return nil
}

func (m *Resource) GetAds() []*ADSEntry {
	if m != nil {
		return m.Ads
	}
	return nil
}

// XAttr encodes extended attributes for a resource.
type XAttr struct {
	// Name specifies the attribute name.
	Name string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
	// Data specifies the associated data for the attribute.
	Data []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
}

func (m *XAttr) Reset()                    { *m = XAttr{} }
func (m *XAttr) String() string            { return proto1.CompactTextString(m) }
func (*XAttr) ProtoMessage()               {}
func (*XAttr) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

// ADSEntry encodes information for a Windows Alternate Data Stream.
type ADSEntry struct {
	// Name specifices the stream name.
	Name string `protobuf:"bytes,1,opt,name=name" json:"name,omitempty"`
	// Data specifies the stream data.
	// See also the description about the digest below.
	Data []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
	// Digest is a CAS representation of the stream data.
	//
	// At least one of data or digest MUST be specified, and either one of them
	// SHOULD be specified.
	//
	// How to access the actual data using the digest is implementation-specific,
	// and implementations can choose not to implement digest.
	// So, digest SHOULD be used only when the stream data is large.
	Digest string `protobuf:"bytes,3,opt,name=digest" json:"digest,omitempty"`
}

func (m *ADSEntry) Reset()                    { *m = ADSEntry{} }
func (m *ADSEntry) String() string            { return proto1.CompactTextString(m) }
func (*ADSEntry) ProtoMessage()               {}
func (*ADSEntry) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func init() {
	proto1.RegisterType((*Manifest)(nil), "proto.Manifest")
	proto1.RegisterType((*Resource)(nil), "proto.Resource")
	proto1.RegisterType((*XAttr)(nil), "proto.XAttr")
	proto1.RegisterType((*ADSEntry)(nil), "proto.ADSEntry")
}

func init() { proto1.RegisterFile("manifest.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 317 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0x8c, 0x90, 0x4f, 0x4b, 0xf3, 0x40,
	0x10, 0xc6, 0x49, 0x93, 0xf4, 0x4d, 0xa7, 0xed, 0xab, 0x2c, 0x52, 0xe6, 0x18, 0x73, 0x0a, 0x08,
	0x15, 0xf4, 0xe0, 0xb9, 0xa2, 0x17, 0xc1, 0xcb, 0x7a, 0xf1, 0xba, 0xba, 0x6b, 0x5c, 0x21, 0xd9,
	0xb0, 0xd9, 0x80, 0xfa, 0xe5, 0xfc, 0x6a, 0x32, 0xb3, 0x69, 0xd1, 0x9b, 0xa7, 0x3c, 0xcf, 0x6f,
	0xfe, 0x64, 0xf6, 0x81, 0xff, 0xad, 0xea, 0xec, 0x8b, 0x19, 0xc2, 0xb6, 0xf7, 0x2e, 0x38, 0x91,
	0xf3, 0xa7, 0xba, 0x82, 0xe2, 0x7e, 0x2a, 0x88, 0x33, 0x28, 0xbc, 0x19, 0xdc, 0xe8, 0x9f, 0x0d,
	0x26, 0x65, 0x5a, 0x2f, 0x2f, 0x8e, 0x62, 0xf3, 0x56, 0x4e, 0x58, 0x1e, 0x1a, 0xaa, 0xaf, 0x19,
	0x14, 0x7b, 0x2c, 0x04, 0x64, 0xbd, 0x0a, 0xaf, 0x3c, 0xb5, 0x90, 0xac, 0xc5, 0x31, 0xa4, 0xa3,
	0xd5, 0x38, 0x2b, 0x93, 0x3a, 0x95, 0x24, 0x89, 0x34, 0x56, 0x63, 0x1a, 0x49, 0x63, 0xb5, 0xd8,
	0x40, 0x36, 0x0e, 0xc6, 0x63, 0x56, 0x26, 0xf5, 0xe2, 0x7a, 0x86, 0x89, 0x64, 0x2f, 0x10, 0xf2,
	0xc6, 0xbb, 0xb1, 0xc7, 0xfc, 0x50, 0x88, 0x80, 0xfe, 0xd4, 0x3a, 0x6d, 0x70, 0x5e, 0x26, 0xf5,
	0x5a, 0xb2, 0x26, 0x36, 0xd8, 0x4f, 0x83, 0xff, 0xca, 0xa4, 0xce, 0x24, 0x6b, 0xb1, 0x81, 0xb9,
	0xb6, 0x8d, 0x19, 0x02, 0x16, 0x7c, 0xd3, 0xe4, 0x88, 0x07, 0xe5, 0x1b, 0x13, 0x70, 0x41, 0xab,
	0xe5, 0xe4, 0xc4, 0x09, 0xe4, 0xad, 0x7a, 0x73, 0x1e, 0x81, 0x97, 0x44, 0xc3, 0xd4, 0x76, 0xce,
	0xe3, 0x72, 0xa2, 0x64, 0x44, 0x05, 0xf9, 0xbb, 0x0a, 0xc1, 0xe3, 0x8a, 0x43, 0x5a, 0x4d, 0x21,
	0x3d, 0xee, 0x42, 0xf0, 0x32, 0x96, 0xc4, 0x29, 0xa4, 0x4a, 0x0f, 0xb8, 0xfe, 0x15, 0xe3, 0xee,
	0xe6, 0xe1, 0xb6, 0x0b, 0xfe, 0x43, 0x52, 0xad, 0x3a, 0x87, 0x9c, 0x47, 0xe8, 0xfe, 0x4e, 0xb5,
	0x94, 0x39, 0x5d, 0xc4, 0x9a, 0x98, 0x56, 0x41, 0x71, 0x7c, 0x2b, 0xc9, 0xba, 0xba, 0x83, 0x62,
	0xbf, 0xe1, 0xaf, 0x33, 0x3f, 0x72, 0x48, 0xe3, 0x7b, 0xa3, 0x7b, 0x9a, 0xf3, 0x45, 0x97, 0xdf,
	0x01, 0x00, 0x00, 0xff, 0xff, 0xef, 0x27, 0x99, 0xf7, 0x17, 0x02, 0x00, 0x00,
}
