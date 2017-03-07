# continuity

[![GoDoc](https://godoc.org/github.com/stevvooe/continuity?status.svg)](https://godoc.org/github.com/stevvooe/continuity)
[![Build Status](https://travis-ci.org/stevvooe/continuity.svg?branch=master)](https://travis-ci.org/stevvooe/continuity)

A transport-agnostic, filesystem metadata manifest system

This project is a staging area for experiments in providing transport agnostic
metadata storage.

Please see https://github.com/opencontainers/specs/issues/11 for more details.

## Building Proto Package

If you change the proto file you will need to rebuild the generated Go with `go generate`.

```
go generate ./proto
```
