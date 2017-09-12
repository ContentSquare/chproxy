pkgs = $(shell go list ./...)

install:
	go get golang.org/x/crypto/acme/autocert
	go get github.com/prometheus/client_golang/prometheus

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