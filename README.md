# continuity

[![Go Reference](https://pkg.go.dev/badge/github.com/containerd/continuity.svg)](https://pkg.go.dev/github.com/containerd/continuity)
[![Build Status](https://github.com/containerd/continuity/workflows/Continuity/badge.svg)](https://github.com/containerd/continuity/actions?query=workflow%3AContinuity+branch%3Amain)

Filesystem utilities for container runtimes and image builders.

## Packages

- [fs](fs): copying, diffing, and disk usage of directory trees
- [fs/fstest](fs/fstest): helpers for creating and comparing directory trees in tests
- [tarconv](tarconv): conversion of tar archives to EROFS images
- [pkg/ext4](pkg/ext4): creation of ext4 filesystem images
- [sysx](sysx): extended attribute system calls
- [devices](devices): device node helpers
- [driver](driver): filesystem driver abstraction

## Platforms

continuity primarily targets Linux. Continuity may compile for and work on
other operating systems, but those platforms are not tested.

## Project details

continuity is a containerd sub-project, licensed under the [Apache 2.0 license](./LICENSE).
As a containerd sub-project, you will find the:
 * [Project governance](https://github.com/containerd/project/blob/main/GOVERNANCE.md),
 * [Maintainers](https://github.com/containerd/project/blob/main/MAINTAINERS),
 * and [Contributing guidelines](https://github.com/containerd/project/blob/main/CONTRIBUTING.md)

information in our [`containerd/project`](https://github.com/containerd/project) repository.
