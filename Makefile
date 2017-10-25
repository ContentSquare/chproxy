pkgs = $(shell go list ./...)

install:
	go get golang.org/x/crypto/acme/autocert
	go get github.com/prometheus/client_golang/prometheus
	go get gopkg.in/yaml.v2
	go get github.com/valyala/bytebufferpool

format:
	go fmt $(pkgs)

build:
	go build

test: build
	go test -httptest.serve=127.0.0.1:8124 -race -v $(pkgs)

run: build
	./chproxy -config=testdata/http.yml

reconfigure:
	kill -HUP `pidof chproxy`