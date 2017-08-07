package main

import (
	"net/url"
	"log"
	"net/http"
	"flag"
	"github.com/hagen1778/chproxy/config"
)

var (
	addr = flag.String("h", "http://localhost:8123", "ClickHouse web-interface host:port address with scheme")
	port = flag.String("p", "8080", "Proxy addr to listen to for incoming requests")
	configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")
)

func main() {
	proxyURL, err := url.Parse(*addr)
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := config.Load(*configFile)

	handler := NewReverseProxy(proxyURL)
	handler.ApplyConfig(cfg)
	log.Fatal(http.ListenAndServe(*port, handler))
}
