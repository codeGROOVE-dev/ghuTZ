.PHONY: all build test lint clean run serve docker-build docker-push deploy

all: lint test build

build:
	go build -o out/ghutz ./cmd/ghutz

test:
	go test -v -race ./...

clean:
	rm -rf out/

run: build
	./out/ghutz $(ARGS)

serve: build
	./out/ghutz --serve

deps:
	go mod tidy
	go mod download

install:
	go install ./cmd/ghutz

# Cloud Run deployment with ko
KO_DOCKER_REPO ?= gcr.io/$(shell gcloud config get-value project)

docker-build:
	@if ! command -v ko &> /dev/null; then \
		echo "Installing ko..."; \
		go install github.com/google/ko@latest; \
	fi
	KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build ./cmd/ghutz

docker-push: docker-build
	@echo "Image built and pushed to $(KO_DOCKER_REPO)"

deploy: docker-push
	gcloud run deploy ghutz \
		--image=$(shell KO_DOCKER_REPO=$(KO_DOCKER_REPO) ko build ./cmd/ghutz) \
		--platform=managed \
		--region=us-central1 \
		--allow-unauthenticated \
		--set-env-vars="GOOGLE_CLOUD_PROJECT=$(shell gcloud config get-value project)" \
		--memory=512Mi \
		--max-instances=10
# BEGIN: lint-install .
# http://github.com/codeGROOVE-dev/lint-install

.PHONY: lint
lint: _lint

LINT_ARCH := $(shell uname -m)
LINT_OS := $(shell uname)
LINT_OS_LOWER := $(shell echo $(LINT_OS) | tr '[:upper:]' '[:lower:]')
LINT_ROOT := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# shellcheck and hadolint lack arm64 native binaries: rely on x86-64 emulation
ifeq ($(LINT_OS),Darwin)
	ifeq ($(LINT_ARCH),arm64)
		LINT_ARCH=x86_64
	endif
endif

LINTERS :=
FIXERS :=

GOLANGCI_LINT_CONFIG := $(LINT_ROOT)/.golangci.yml
GOLANGCI_LINT_VERSION ?= v2.4.0
GOLANGCI_LINT_BIN := $(LINT_ROOT)/out/linters/golangci-lint-$(GOLANGCI_LINT_VERSION)-$(LINT_ARCH)
$(GOLANGCI_LINT_BIN):
	mkdir -p $(LINT_ROOT)/out/linters
	rm -rf $(LINT_ROOT)/out/linters/golangci-lint-*
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LINT_ROOT)/out/linters $(GOLANGCI_LINT_VERSION)
	mv $(LINT_ROOT)/out/linters/golangci-lint $@

LINTERS += golangci-lint-lint
golangci-lint-lint: $(GOLANGCI_LINT_BIN)
	"$(GOLANGCI_LINT_BIN)" run -c "$(GOLANGCI_LINT_CONFIG)" ./...

FIXERS += golangci-lint-fix
golangci-lint-fix: $(GOLANGCI_LINT_BIN)
	find . -name go.mod -execdir "$(GOLANGCI_LINT_BIN)" run -c "$(GOLANGCI_LINT_CONFIG)" --fix \;

YAMLLINT_VERSION ?= 1.37.1
YAMLLINT_ROOT := $(LINT_ROOT)/out/linters/yamllint-$(YAMLLINT_VERSION)
YAMLLINT_BIN := $(YAMLLINT_ROOT)/dist/bin/yamllint
$(YAMLLINT_BIN):
	mkdir -p $(LINT_ROOT)/out/linters
	rm -rf $(LINT_ROOT)/out/linters/yamllint-*
	curl -sSfL https://github.com/adrienverge/yamllint/archive/refs/tags/v$(YAMLLINT_VERSION).tar.gz | tar -C $(LINT_ROOT)/out/linters -zxf -
	cd $(YAMLLINT_ROOT) && pip3 install --target dist . || pip install --target dist .

LINTERS += yamllint-lint
yamllint-lint: $(YAMLLINT_BIN)
	PYTHONPATH=$(YAMLLINT_ROOT)/dist $(YAMLLINT_ROOT)/dist/bin/yamllint .

.PHONY: _lint $(LINTERS)
_lint: $(LINTERS)

.PHONY: fix $(FIXERS)
fix: $(FIXERS)

# END: lint-install .
