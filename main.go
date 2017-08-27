package main

import (
	"crypto/tls"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
)

var configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")

var proxy *reverseProxy

func main() {
	flag.Parse()

	log.Infof("Loading config: %s", *configFile)
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		log.Fatalf("can't load config %q: %s", *configFile, err)
	}
	log.Infof("Loading config: %s", "success")

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

	if cfg.ListenTLSAddr != "" {
		startTLS(cfg.ListenTLSAddr, cfg.CertCacheDir)
	}

	log.Infof("Serving http on %q", cfg.ListenAddr)
	server := &http.Server{Addr: cfg.ListenAddr, Handler: handler}
	log.Fatalf("Server error: %s", server.ListenAndServe())
}

var handler = &httpHandler{}

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

func startTLS(tlsAddr, dir string) {
	ln, err := net.Listen("tcp4", tlsAddr)
	if err != nil {
		log.Fatalf("cannot listen for -tlsAddr=%q: %s", tlsAddr, err)
	}

	tlsConfig := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}

	m := autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(dir),
	}
	tlsConfig.GetCertificate = m.GetCertificate

	tlsLn := tls.NewListener(ln, &tlsConfig)

	log.Infof("Serving https on %q", tlsAddr)
	go http.Serve(tlsLn, handler)
}
