#   Copyright The containerd Authors.

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# Set an output prefix, which is the local directory if not specified
PREFIX?=$(shell pwd)

PKG=github.com/containerd/continuity

PACKAGES=$(shell go list -mod=vendor ./... | grep -v /vendor/)
TEST_REQUIRES_ROOT_PACKAGES=$(filter \
    ${PACKAGES}, \
    $(shell \
    for f in $$(git grep -l testutil.RequiresRoot | grep -v Makefile); do \
        d="$$(dirname $$f)"; \
        [ "$$d" = "." ] && echo "${PKG}" && continue; \
        echo "${PKG}/$$d"; \
    done | sort -u) \
    )

GO_TEST_FLAGS?=
GO_LDFLAGS?=

.PHONY: clean all lint build test codecov binaries
.DEFAULT: default

all: AUTHORS clean lint build test binaries

AUTHORS: .mailmap .git/HEAD
	 git log --format='%aN <%aE>' | sort -fu > $@

${PREFIX}/bin/continuity:
	@echo "+ $@"
	@(cd cmd/continuity && go build -mod=mod -o $@  ${GO_GCFLAGS} .)

generate:
	go generate -mod=vendor $(PACKAGES)

lint:
	@echo "+ $@"
	@golangci-lint run

build:
	@echo "+ $@"
	@go build -mod=vendor -v ${GO_LDFLAGS} $(PACKAGES)

test:
	@echo "+ $@"
	@go test -mod=vendor $(PACKAGES) $(GO_TEST_FLAGS)

root-test:
	@echo "+ $@"
	@go test -exec sudo ${TEST_REQUIRES_ROOT_PACKAGES} -test.root $(GO_TEST_FLAGS)

.PHONY: $(PREFIX)/codecov/unit
$(PREFIX)/codecov/unit: GO_TEST_FLAGS+=-cover -covermode=atomic -args -test.gocoverdir=$@
$(PREFIX)/codecov/unit:
	@mkdir -p $@
	@$(MAKE) test GO_TEST_FLAGS="$(GO_TEST_FLAGS)"

.PHONY: $(PREFIX)/codecov/root
$(PREFIX)/codecov/root: GO_TEST_FLAGS+=-cover -covermode=atomic -args -test.gocoverdir=$@
$(PREFIX)/codecov/root:
	@mkdir -p $@
	@$(MAKE) root-test GO_TEST_FLAGS="$(GO_TEST_FLAGS)"

codecov: $(PREFIX)/codecov/unit $(PREFIX)/codecov/root
	@go tool covdata textfmt \
		-i=$(PREFIX)/codecov/unit,$(PREFIX)/codecov/root \
		-o $(PREFIX)/codecov/report.txt
	@go tool cover -html=$(PREFIX)/codecov/report.txt -o $(PREFIX)/codecov/report.html

test-compile:
	@echo "+ $@"
	@for pkg in $(PACKAGES); do go test -mod=vendor -c $$pkg; done

binaries: ${PREFIX}/bin/continuity
	@echo "+ $@"
	@if [ x$$GOOS = xwindows ]; then echo "+ continuity -> continuity.exe"; mv ${PREFIX}/bin/continuity ${PREFIX}/bin/continuity.exe; fi

clean:
	@echo "+ $@"
	@rm -rf "${PREFIX}/bin/continuity" "${PREFIX}/bin/continuity.exe"
	@rm -rf codecov/

