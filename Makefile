pkgs = $(shell go list ./...)

format:
	go fmt $(pkgs)

build:
	go build

test: build
	go test -v $(pkgs)

run: build
	./chproxy --debug=true