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

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
)

var configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")

var (
	proxy           = newReverseProxy()
	allowedNetworks atomic.Value
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

	if cfg.IsTLS {
		if err := serveTLS(cfg.ListenAddr, &cfg.TLSConfig); err != nil {
			log.Fatalf("TLS server error on %q: %s", cfg.ListenAddr, err)
		}
		return
	}

	if err := serve(cfg.ListenAddr); err != nil {
		log.Fatalf("Server error on %q: %s", cfg.ListenAddr, err)
	}
}

func serveTLS(addr string, tlsConfig *config.TLSConfig) error {
	ln := newTLSListener(addr, tlsConfig)

	log.Infof("Serving https on %q", addr)
	return listenAndServe(ln)
}

func serve(addr string) error {
	ln := newListener(addr)

	log.Infof("Serving http on %q", addr)
	return listenAndServe(ln)
}

func newListener(laddr string) net.Listener {
	ln, err := net.Listen("tcp4", laddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", laddr, err)
	}
	return &netListener{
		ln,
	}
}

func newTLSListener(laddr string, cfg *config.TLSConfig) net.Listener {
	tlsConfig := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}

	if len(cfg.KeyFile) > 0 && len(cfg.CertFile) > 0 {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			log.Fatalf("cannot load cert for `tls_config.cert_file`=%q, `tls_config.key_file`=%q: %s",
				cfg.CertFile, cfg.KeyFile, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	} else {
		if len(cfg.CertCacheDir) > 0 {
			if err := os.MkdirAll(cfg.CertCacheDir, 0700); err != nil {
				log.Fatalf("error while creating folder %q: %s", cfg.CertCacheDir, err)
			}
		}

		var hp autocert.HostPolicy
		if len(cfg.HostPolicy) != 0 {
			allowedHosts := make(map[string]struct{}, len(cfg.HostPolicy))
			for _, v := range cfg.HostPolicy {
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
			Cache:      autocert.DirCache(cfg.CertCacheDir),
			HostPolicy: hp,
		}

		tlsConfig.GetCertificate = m.GetCertificate
	}

	ln := newListener(laddr)
	return tls.NewListener(ln, &tlsConfig)
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
}

func (ln *netListener) Accept() (net.Conn, error) {
	for {
		conn, err := ln.Listener.Accept()
		if err != nil {
			return nil, err
		}

		remoteAddr := conn.RemoteAddr().String()
		an := allowedNetworks.Load().(*config.Networks)
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

	allowedNetworks.Store(&cfg.Networks)
	log.SetDebug(cfg.LogDebug)
	return &cfg.Server, nil
}
