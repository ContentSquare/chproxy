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

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
	"time"
)

var configFile = flag.String("config", "proxy.yml", "Proxy configuration filename")

var (
	proxy    *reverseProxy
	networks atomic.Value
)

func main() {
	flag.Parse()

	log.Infof("Loading config: %s", *configFile)
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		log.Fatalf("can't load config %q: %s", *configFile, err)
	}
	log.Infof("Loading config %q: success", *configFile)

	proxy = NewReverseProxy()
	if err := reloadConfig(cfg); err != nil {
		log.Fatalf("error while loading config: %s", err)
	}

	c := make(chan os.Signal)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		for {
			switch <-c {
			case syscall.SIGHUP:
				log.Infof("SIGHUP received. Going to reload config %s ...", *configFile)
				cfg, err := config.LoadFile(*configFile)
				if err != nil {
					log.Errorf("can't load config %q: %s", *configFile, err)
					continue
				}

				if err := reloadConfig(cfg); err != nil {
					log.Errorf("error while reloading config: %s", err)
					continue
				}

				log.Infof("Config successfully reloaded")
			}
		}
	}()

	if cfg.ListenTLSAddr != "" {
		go listenAndServe(cfg, true)
	}

	listenAndServe(cfg, false)
}

var promHandler = promhttp.Handler()

func serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		promHandler.ServeHTTP(rw, r)
	case "/":
		proxy.ServeHTTP(rw, r)
	}
}

func listenAndServe(cfg *config.Config, isTLS bool) {
	s := &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      http.HandlerFunc(serveHTTP),
		ReadTimeout: time.Minute,
		WriteTimeout: time.Minute,
		IdleTimeout: time.Minute * 10,
		ErrorLog: log.ErrorLogger,
	}

	var ln net.Listener
	var serveInfo string
	if !isTLS {
		ln = newListener(cfg.ListenAddr)
		serveInfo = fmt.Sprintf("http on %q", cfg.ListenAddr)
	} else {
		ln = newTLSListener(cfg.ListenTLSAddr, cfg.CertCacheDir, cfg.HostPolicy)
		serveInfo = fmt.Sprintf("https on %q", cfg.ListenTLSAddr)
	}

	log.Infof("Serving %s", serveInfo)
	if err := s.Serve(ln); err != nil {
		log.Fatalf("Server error %s: %s", serveInfo, err)
	}
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

func newTLSListener(laddr, certCacheDir string, hostPolicy []string) net.Listener {
	var hp autocert.HostPolicy
	if len(hostPolicy) != 0 {
		allowedHosts := make(map[string]struct{}, len(hostPolicy))
		for _, v := range hostPolicy {
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
		Cache:      autocert.DirCache(certCacheDir),
		HostPolicy: hp,
	}

	ln := newListener(laddr)
	tlsConfig := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}
	tlsConfig.GetCertificate = m.GetCertificate
	return tls.NewListener(ln, &tlsConfig)
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
		allowedNetworks := networks.Load().(*config.Networks)
		if !allowedNetworks.Contains(remoteAddr) {
			log.Errorf("connections are not allowed from %s", remoteAddr)
			conn.Close()
			continue
		}

		return conn, nil
	}
}

func reloadConfig(cfg *config.Config) error {
	if err := proxy.ApplyConfig(cfg); err != nil {
		return err
	}
	networks.Store(&cfg.Networks)
	log.SetDebug(cfg.LogDebug)

	return nil
}
