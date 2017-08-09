package main

import (
	"log"
	"net/http"
	"flag"
	"github.com/hagen1778/chproxy/config"
)

var (
	addr = flag.String("h", "http://localhost:8123", "ClickHouse web-interface host:port address with scheme")
	port = flag.String("p", ":8080", "Proxy addr to listen to for incoming requests")
	configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")
)

func main() {
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		log.Fatalf("can't load config %q: %s", *configFile, err)
	}

	handler, err := NewReverseProxy(cfg)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/favicon.ico", serveFavicon)
	http.HandleFunc("/", handler.ServeHTTP)
	log.Fatal(http.ListenAndServe(*port, nil))
}

func serveFavicon(w http.ResponseWriter, r *http.Request) {}