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
	configFile    = flag.String("config", "proxy.yml", "Proxy configuration filename")
)

func main() {
	flag.Parse()

	log.Debugf("Loading config: %s", *configFile)
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		log.Fatalf("can't load config %q: %s", *configFile, err)
	}
	log.Debugf("Loading config: %s", "success")

	proxy, err := NewReverseProxy(cfg)
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

	http.HandleFunc("/favicon.ico", serveFavicon)
	http.HandleFunc("/metrics", promhttp.Handler().ServeHTTP)
	http.HandleFunc("/", proxy.ServeHTTP)

	log.Infof("Start listening on %s", *listenAddr)
	log.Fatalf("error while listening on %s: %s", *listenAddr, http.ListenAndServe(*listenAddr, nil))
}

func serveFavicon(w http.ResponseWriter, r *http.Request) {}
