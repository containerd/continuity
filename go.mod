module github.com/containerd/continuity

go 1.13

require (
	// 5883e5a4b512fe2e32f915b1c66a1ddfef81cb3f is the last version to support macOS
	// see https://github.com/bazil/fuse/commit/60eaf8f021ce00e5c52529cdcba1067e13c1c2c2
	bazil.org/fuse v0.0.0-20200407214033-5883e5a4b512
	github.com/Microsoft/go-winio v0.5.1
	github.com/dustin/go-humanize v1.0.0
	github.com/golang/protobuf v1.5.2
	github.com/opencontainers/go-digest v1.0.0
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.3.0
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20211205182925-97ca703d548d
)
