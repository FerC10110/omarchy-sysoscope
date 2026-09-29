VERSION := $(shell cat collector/VERSION)
GOARCH ?= $(shell go env GOARCH)
FLAGS := -trimpath -buildvcs=false -ldflags="-s -w -buildid= -X main.version=$(VERSION)"
ARCHES := amd64 arm64

.PHONY: test build bin verify-bin

test:
	cd collector && go vet ./... && go test -race ./...
	node --test tests/

build:
	cd collector && CGO_ENABLED=0 GOARCH=$(GOARCH) go build $(FLAGS) -o ../bin/sysoscope-linux-$(GOARCH) ./cmd/sysoscope

bin:
	@for a in $(ARCHES); do \
		(cd collector && CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build $(FLAGS) -o ../bin/sysoscope-linux-$$a ./cmd/sysoscope) || exit 1; \
	done
	cd bin && sha256sum sysoscope-* > SHA256SUMS

verify-bin:
	@for a in $(ARCHES); do \
		(cd collector && CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build $(FLAGS) -o dist/sysoscope-linux-$$a ./cmd/sysoscope) || exit 1; \
		cmp collector/dist/sysoscope-linux-$$a bin/sysoscope-linux-$$a || { echo "bin/sysoscope-linux-$$a is not reproducible from source"; exit 1; }; \
	done
	cd bin && sha256sum -c --quiet SHA256SUMS
