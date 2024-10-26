module github.com/containerd/continuity/cmd/continuity

go 1.21

require (
	bazil.org/fuse v0.0.0-20230120002735-62a210ff1fd5
	github.com/containerd/continuity v0.0.0-00010101000000-000000000000 // see replace
	github.com/containerd/log v0.1.0
	github.com/dustin/go-humanize v1.0.1
	github.com/golang/protobuf v1.5.4
	github.com/opencontainers/go-digest v1.0.0
	github.com/spf13/cobra v1.8.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/sys v0.26.0 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
)

// use local source for the main module
replace github.com/containerd/continuity => ../../
