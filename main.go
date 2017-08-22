package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	listenAddr = flag.String("listenAddr", ":8080", "Proxy addr to listen to for incoming requests")
	configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")
)

var proxy *reverseProxy

func main() {
	flag.Parse()

	log.Debugf("Loading config: %s", *configFile)
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		log.Fatalf("can't load config %q: %s", *configFile, err)
	}
	log.Debugf("Loading config: %s", "success")

	proxy, err = NewReverseProxy(cfg)
	if err != nil {
		log.Fatalf("error while creating proxy: %s", err)
	}

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		for {
			s := <-c
			switch s {
			case syscall.SIGHUP:
				log.Infof("SIGHUP received. Going to reload config %s ...", *configFile)
				if err := proxy.ReloadConfig(*configFile); err != nil {
					log.Errorf("error while reloading config: %s", err)
				}
				log.Infof("Config successfully reloaded")
			}
		}
	}()

	handler := &httpHandler{}
	server := &http.Server{Addr: *listenAddr, Handler: handler}
	log.Infof("Start listening on %s", *listenAddr)
	server.ListenAndServe()
}

type httpHandler struct{}

func (h *httpHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		promhttp.Handler().ServeHTTP(rw, r)
	case "/":
		proxy.ServeHTTP(rw, r)
	}
}


