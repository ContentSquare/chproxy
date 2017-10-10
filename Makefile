pkgs = $(shell go list ./...)

install:
	go get golang.org/x/crypto/acme/autocert
	go get github.com/prometheus/client_golang/prometheus
	go get gopkg.in/yaml.v2

format:
	go fmt $(pkgs)

build:
	go build

test: build
	go test -race -v $(pkgs)

run: build
	./chproxy -config=testdata/http.conf.yml

reconfigure:
	kill -HUP `pidof chproxy`