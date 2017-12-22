pkgs = $(shell go list ./...)

BUILD_TAG = $(shell git tag --points-at HEAD)

BUILD_CONSTS = \
	-X main.buildTime=`date -u '+%Y-%m-%d_%H:%M:%S'` \
	-X main.buildRevision=`git rev-parse HEAD` \
	-X main.buildTag=$(BUILD_TAG)

BUILD_OPTS = -ldflags="$(BUILD_CONSTS)" -gcflags="-trimpath=$(GOPATH)/src"

update:
	dep ensure -update

format:
	dep ensure
	go fmt $(pkgs)
	gofmt -w -s .

build:
	go build

test: build
	go test -race $(pkgs)

run: build
	./chproxy -config=testdata/http.yml

lint:
	go vet $(pkgs)
	golint ./...

reconfigure:
	kill -HUP `pidof chproxy`

release: format lint test
	rm -f chproxy
	GOOS=linux GOARCH=amd64 go build $(BUILD_OPTS)
	tar czf chproxy-linux-amd64-$(BUILD_TAG).tar.gz chproxy
