GOLANGCI_VERSION = v2.13.2
RETROGOLINT_VERSION = v1.0.3

help: ## show help, shown by default if no target is specified
	@grep -E '^[0-9a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

lint: ## run code linters
	golangci-lint run
	retrogolint

build-all: ## build code
	go build ./...

test: install ## run tests
	go test -timeout 10s -race ./...

test-coverage: ## run unit tests and create test coverage
	go test -timeout 10s ./... -coverprofile coverage.txt

test-coverage-web: test-coverage ## run unit tests and show test coverage in browser
	go tool cover -func coverage.txt | grep total | awk '{print "Total coverage: "$$3}'
	go tool cover -html=coverage.txt

install: ## install all binaries
	go install -buildvcs=false .

install-linters: ## install all used linters
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}
	go install github.com/retroenv/retrogolint/cmd/retrogolint@${RETROGOLINT_VERSION}

release: ## build release binaries for current git tag and publish on github
	goreleaser release

release-snapshot: ## build release binaries from current git state as snapshot
	goreleaser release --snapshot --clean
