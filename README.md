# continuity

[![Go Reference](https://pkg.go.dev/badge/github.com/containerd/continuity.svg)](https://pkg.go.dev/github.com/containerd/continuity)
[![Build Status](https://github.com/containerd/continuity/workflows/Continuity/badge.svg)](https://github.com/containerd/continuity/actions?query=workflow%3AContinuity+branch%3Amain)

A transport-agnostic, filesystem metadata manifest system

This project is a staging area for experiments in providing transport agnostic
metadata storage.

See [opencontainers/runtime-spec#11](https://github.com/opencontainers/runtime-spec/issues/11)
for more details.

## Manifest Format

A continuity manifest encodes filesystem metadata in Protocol Buffers.
Refer to [proto/manifest.proto](proto/manifest.proto) for more details.

## Platforms

continuity primarily targets Linux. Continuity may compile for and work on
other operating systems, but those platforms are not tested.

## Contribution Guide
### Building Proto Package

If you change the proto file you will need to rebuild the generated Go with `go generate`.

```console
$ go generate ./proto
```

## Project details

continuity is a containerd sub-project, licensed under the [Apache 2.0 license](./LICENSE).
As a containerd sub-project, you will find the:
 * [Project governance](https://github.com/containerd/project/blob/main/GOVERNANCE.md),
 * [Maintainers](https://github.com/containerd/project/blob/main/MAINTAINERS),
 * and [Contributing guidelines](https://github.com/containerd/project/blob/main/CONTRIBUTING.md)

information in our [`containerd/project`](https://github.com/containerd/project) repository.
