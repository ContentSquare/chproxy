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

	http.HandleFunc("/", serveHTTP)

	if cfg.ListenTLSAddr != "" {
		startTLS(cfg)
	}

	ln, err := newListener(cfg.ListenAddr, cfg.Networks)
	if err != nil {
		log.Fatalf("cannot listen for -addr=%q: %s", cfg.ListenAddr, err)
	}

	log.Infof("Serving http on %q", cfg.ListenAddr)
	log.Fatalf("Server error: %s", newServer().Serve(ln))
}

func serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		promhttp.Handler().ServeHTTP(rw, r)
	case "/":
		proxy.ServeHTTP(rw, r)
	}
}

func startTLS(cfg *config.Config) {
	ln, err := newListener(cfg.ListenTLSAddr, cfg.Networks)
	if err != nil {
		log.Fatalf("cannot listen for -tlsAddr=%q: %s", cfg.ListenTLSAddr, err)
	}

	tlsConfig := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}

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
	tlsConfig.GetCertificate = m.GetCertificate

	tlsLn := tls.NewListener(ln, &tlsConfig)

	log.Infof("Serving https on %q", cfg.ListenTLSAddr)
	go log.Fatalf("TLS Server error: %s", newServer().Serve(tlsLn))
}

type netListener struct {
	net.Listener

	allowedNetworks config.Networks
}

func newListener(laddr string, allowedNetworks config.Networks) (*netListener, error) {
	ln, err := net.Listen("tcp4", laddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", laddr, err)
	}

	return &netListener{
		Listener:        ln,
		allowedNetworks: allowedNetworks,
	}, nil
}

func (ln *netListener) Accept() (net.Conn, error) {
	for {
		conn, err := ln.Listener.Accept()
		if err != nil {
			return nil, err
		}

		remoteAddr := conn.RemoteAddr().String()
		ok, err := ln.allowedNetworks.Allowed(remoteAddr)
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

func newServer() *http.Server {
	return &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
}
