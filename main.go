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
	"regexp"
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

	log.SetDebug(cfg.LogDebug)

	if proxy, err = NewReverseProxy(cfg); err != nil {
		log.Fatalf("error while creating proxy: %s", err)
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
					return
				}

				if err := proxy.ApplyConfig(cfg); err != nil {
					log.Errorf("error while reloading config: %s", err)
					return
				}

				log.SetDebug(cfg.LogDebug)
				log.Infof("Config successfully reloaded")
			}
		}
	}()

	if cfg.ListenTLSAddr != "" {
		log.Infof("Serving https on %q", cfg.ListenTLSAddr)
		go listenAndServe(cfg, true)
	}

	log.Infof("Serving http on %q", cfg.ListenAddr)
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
	var ln net.Listener
	if !isTLS {
		ln = newListener(cfg.ListenAddr, cfg.Networks)
	} else {
		ln = newTLSListener(cfg)
	}

	s := &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      http.HandlerFunc(serveHTTP),
	}

	log.Fatalf("Server error: %s", s.Serve(ln))
}

func newListener(laddr string, allowedNetworks config.Networks) *netListener {
	ln, err := net.Listen("tcp4", laddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", laddr, err)
	}

	return &netListener{
		Listener:        ln,
		allowedNetworks: allowedNetworks,
	}
}

func newTLSListener(cfg *config.Config) net.Listener {
	var hostPolicy autocert.HostPolicy
	if len(cfg.HostPolicyRegexp) != 0 {
		hostPolicyRegexp, err := regexp.Compile(cfg.HostPolicyRegexp)
		if err != nil {
			log.Fatalf("cannot compile `host_policy_regexp`=%q: %s", cfg.HostPolicyRegexp, err)
		}

		hostPolicy = func(_ context.Context, host string) error {
			if hostPolicyRegexp.MatchString(host) {
				return nil
			}

			return fmt.Errorf("host %q doesn't match `host_policy_regexp` %q", host, cfg.HostPolicyRegexp)
		}
	}

	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cfg.CertCacheDir),
		HostPolicy: hostPolicy,
	}

	ln := newListener(cfg.ListenTLSAddr, cfg.Networks)
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

	allowedNetworks config.Networks
}

func (ln *netListener) Accept() (net.Conn, error) {
	for {
		conn, err := ln.Listener.Accept()
		if err != nil {
			return nil, err
		}

		remoteAddr := conn.RemoteAddr().String()
		ok, err := ln.allowedNetworks.Contains(remoteAddr)
		if err != nil {
			log.Errorf("listener allowed networks err: %s", err)
			conn.Close()
			continue
		}

		if !ok {
			log.Errorf("connections are not allowed from %s", remoteAddr)
			conn.Close()
			continue
		}

		return conn, nil
	}
}
