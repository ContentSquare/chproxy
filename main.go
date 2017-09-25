package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
)

var configFile = flag.String("config", "testdata/http.conf.yml", "Proxy configuration filename")

var (
	proxy = newReverseProxy()

	allowedNetworksHTTP    atomic.Value
	allowedNetworksHTTPS   atomic.Value
	allowedNetworksMetrics atomic.Value
)

func main() {
	flag.Parse()

	log.Infof("Loading config: %s", *configFile)
	cfg, err := reloadConfig()
	if err != nil {
		log.Fatalf("error while loading config: %s", err)
	}
	log.Infof("Loading config %s: successful", *configFile)

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		for {
			switch <-c {
			case syscall.SIGHUP:
				log.Infof("SIGHUP received. Going to reload config %s ...", *configFile)
				if _, err := reloadConfig(); err != nil {
					log.Errorf("error while reloading config: %s", err)
					continue
				}
				log.Infof("Reloading config %s: successful", *configFile)
			}
		}
	}()

	if len(cfg.HTTPS.ListenAddr) != 0 {
		go serveTLS(cfg.HTTPS)
	}

	if len(cfg.HTTP.ListenAddr) != 0 {
		go serve(cfg.HTTP)
	}

	select {}
}

func serveTLS(cfg config.HTTPS) {
	ln := newTLSListener(cfg)
	log.Infof("Serving https on %q", cfg.ListenAddr)
	if err := listenAndServe(ln); err != nil {
		log.Fatalf("TLS server error on %q: %s", cfg.ListenAddr, err)
	}
}
func serve(cfg config.HTTP) {
	an := func() *config.Networks {
		return allowedNetworksHTTP.Load().(*config.Networks)
	}
	ln := newListener(cfg.ListenAddr, an)
	log.Infof("Serving http on %q", cfg.ListenAddr)
	if err := listenAndServe(ln); err != nil {
		log.Fatalf("HTTP server error on %q: %s", cfg.ListenAddr, err)
	}
}

func newListener(laddr string, an func() *config.Networks) net.Listener {
	ln, err := net.Listen("tcp4", laddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", laddr, err)
	}
	return &netListener{
		Listener:        ln,
		allowedNetworks: an,
	}
}

func newTLSListener(cfg config.HTTPS) net.Listener {
	tlsCfg := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}

	if len(cfg.KeyFile) > 0 && len(cfg.CertFile) > 0 {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			log.Fatalf("cannot load cert for `https.cert_file`=%q, `https.key_file`=%q: %s",
				cfg.CertFile, cfg.KeyFile, err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	} else {
		if len(cfg.Autocert.CacheDir) > 0 {
			if err := os.MkdirAll(cfg.Autocert.CacheDir, 0700); err != nil {
				log.Fatalf("error while creating folder %q: %s", cfg.Autocert.CacheDir, err)
			}
		}

		var hp autocert.HostPolicy
		if len(cfg.Autocert.AllowedHosts) != 0 {
			allowedHosts := make(map[string]struct{}, len(cfg.Autocert.AllowedHosts))
			for _, v := range cfg.Autocert.AllowedHosts {
				allowedHosts[v] = struct{}{}
			}

			hp = func(_ context.Context, host string) error {
				if _, ok := allowedHosts[host]; ok {
					return nil
				}

				return fmt.Errorf("host %q doesn't match `host_policy` configuration", host)
			}
		}

		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(cfg.Autocert.CacheDir),
			HostPolicy: hp,
		}

		tlsCfg.GetCertificate = m.GetCertificate
	}

	an := func() *config.Networks {
		return allowedNetworksHTTPS.Load().(*config.Networks)
	}
	ln := newListener(cfg.ListenAddr, an)
	return tls.NewListener(ln, &tlsCfg)
}

func listenAndServe(ln net.Listener) error {
	s := &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      http.HandlerFunc(serveHTTP),
		ReadTimeout:  time.Minute,
		WriteTimeout: time.Minute,
		IdleTimeout:  time.Minute * 10,
		ErrorLog:     log.ErrorLogger,
	}

	return s.Serve(ln)
}

var promHandler = promhttp.Handler()

func serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		an := allowedNetworksMetrics.Load().(*config.Networks)
		if !an.Contains(r.RemoteAddr) {
			log.Errorf("connections to /metrics are not allowed from %s", r.RemoteAddr)
			rw.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(rw, "connections to /metrics are not allowed from %s", r.RemoteAddr)
			return
		}
		promHandler.ServeHTTP(rw, r)
	case "/":
		goodRequest.Inc()
		proxy.ServeHTTP(rw, r)
	default:
		badRequest.Inc()
		log.Debugf("Unsupported path: %s", r.URL.Path)
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(rw, "Unsupported path: %s", r.URL.Path)
	}
}

type netListener struct {
	net.Listener
	allowedNetworks func() *config.Networks
}

func (ln *netListener) Accept() (net.Conn, error) {
	for {
		conn, err := ln.Listener.Accept()
		if err != nil {
			return nil, err
		}

		remoteAddr := conn.RemoteAddr().String()
		an := ln.allowedNetworks()
		if !an.Contains(remoteAddr) {
			log.Errorf("connections are not allowed from %s", remoteAddr)
			conn.Close()
			continue
		}

		return conn, nil
	}
}

func reloadConfig() (*config.Server, error) {
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		return nil, fmt.Errorf("can't load config %q: %s", *configFile, err)
	}

	if err := proxy.ApplyConfig(cfg); err != nil {
		return nil, err
	}

	allowedNetworksHTTP.Store(&cfg.Server.HTTP.AllowedNetworks)
	allowedNetworksHTTPS.Store(&cfg.Server.HTTPS.AllowedNetworks)
	allowedNetworksMetrics.Store(&cfg.Server.Metrics.AllowedNetworks)
	log.SetDebug(cfg.LogDebug)
	log.Infof("New config is next: \n%s", cfg)
	return &cfg.Server, nil
}
