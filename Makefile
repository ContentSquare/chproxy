pkgs = $(shell go list ./...)

format:
	go fmt $(pkgs)

build:
	go build

test: build
	go test -race -v $(pkgs)

bench: build
	go test -race -v -bench=. $(pkgs) -benchmem

run: build
	./chproxy

reconfigure:
	kill -HUP `pidof chproxy`