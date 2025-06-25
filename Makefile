# go-baresip Makefile

all: build-examples lint test


# ---------------------------------------------------------------------------- #
#                                 Build & Test                                 #
# ---------------------------------------------------------------------------- #

# Assuming GO is already installed -- see https://golang.org/doc/install if that's not the case
# Assuming golangci-lint is already installed -- see https://golangci-lint.run/usage/install/#installing-golangci-lint if that's not the case

test: | unit-test

unit-test: | lint
	@echo Running unit tests...
	@go test -cover -short -tags testtools ./...

coverage:
	@echo Generating coverage report...
	@go test ./... -race -coverprofile=coverage.out -covermode=atomic -tags testtools -p 1
	@go tool cover -func=coverage.out
	@go tool cover -html=coverage.out

EXAMPLES:=\
	internal-baresip \
	external-baresip \
	basic-nochannels

build-examples:
	@go mod download
	for ex in $(EXAMPLES); do \
		echo "Building [$${ex}] as bin/$${ex}" ; \
		CGO_ENABLED=0 GOOS=linux go build -mod=readonly -v -o bin/$${ex} examples/$${ex}/main.go ; \
	done

clean: docker-clean
	@echo Cleaning...
	@rm -f app
	@rm -rf ./dist/
	@rm -rf coverage.out
	@rm -rf bin
	@go clean -testcache

vendor:
	@go mod download
	@go mod tidy

# ---------------------------------------------------------------------------- #
#                                     Lint                                     #
# ---------------------------------------------------------------------------- #

lint: golint

golint: | vendor
	@echo Linting Go files...
	@golangci-lint run

golint-fix: | vendor
	@echo --- Linting Go files...
	golangci-lint run --fix

fmt:
	go fmt ./...
	# required by the gofumpt linter:
	gofumpt -l -w -extra .