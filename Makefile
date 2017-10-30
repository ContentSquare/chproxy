pkgs = $(shell go list ./...)

BUILD_CONSTS = \
	-X main.buildTime=`date -u '+%Y-%m-%d_%H:%M:%S'` \
	-X main.buildRevision=`git rev-parse HEAD` \
	-X main.buildTag=`git tag --points-at HEAD`

BUILD_OPTS = -ldflags="$(BUILD_CONSTS)"

install:
	go get golang.org/x/crypto/acme/autocert
	go get github.com/prometheus/client_golang/prometheus
	go get gopkg.in/yaml.v2
	go get github.com/valyala/bytebufferpool

format:
	go fmt $(pkgs)

build:
	go build $(BUILD_OPTS)

test: build
	go test -race -v $(pkgs)

run: build
	./chproxy -config=testdata/http.yml

reconfigure:
	kill -HUP `pidof chproxy`

release:
	GOOS=linux GOARCH=amd64 go build $(BUILD_OPTS) -o chproxy-linux-amd64
