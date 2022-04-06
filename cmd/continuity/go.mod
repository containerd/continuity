module github.com/containerd/continuity/cmd/continuity

go 1.17

require (
	// 5883e5a4b512fe2e32f915b1c66a1ddfef81cb3f is the last version to support macOS
	// see https://github.com/bazil/fuse/commit/60eaf8f021ce00e5c52529cdcba1067e13c1c2c2
	bazil.org/fuse v0.0.0-20200407214033-5883e5a4b512
	github.com/containerd/continuity v0.0.0-00010101000000-000000000000 // see replace
	github.com/dustin/go-humanize v1.0.0
	github.com/golang/protobuf v1.3.5
	github.com/opencontainers/go-digest v1.0.0
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.0.0
)

require (
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/spf13/pflag v1.0.3 // indirect
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
)

// use local source for the main module
replace github.com/containerd/continuity => ../../
