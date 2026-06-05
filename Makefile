.PHONY: fmt fmt-check vet test build package clean-dist check

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Files need gofmt:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

test:
	go test ./...

build:
	go build ./...

package:
	./scripts/build-dist.sh

clean-dist:
	rm -rf dist

check: fmt vet test build
