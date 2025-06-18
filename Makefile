# go-baresip Makefile

all: build


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

build-example:
	@go mod download
	CGO_ENABLED=0 GOOS=linux go build -mod=readonly -v -o bin/${APP_NAME} example/main.go

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