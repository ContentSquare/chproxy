pkgs = $(shell go list ./...)

BUILD_TAG = $(shell git tag --points-at HEAD)

BUILD_CONSTS = \
	-X main.buildTime=`date -u '+%Y-%m-%d_%H:%M:%S'` \
	-X main.buildRevision=`git rev-parse HEAD` \
	-X main.buildTag=$(BUILD_TAG)

BUILD_OPTS = -ldflags="$(BUILD_CONSTS)" -gcflags="-trimpath=$(GOPATH)/src"

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

release:
	GOOS=linux GOARCH=amd64 go build $(BUILD_OPTS) -o chproxy-linux-amd64
	tar czf chproxy-linux-amd64-$(BUILD_TAG).tar.gz chproxy-linux-amd64
