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
	proxy    = NewReverseProxy()
	networks atomic.Value
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

	listenAndServe(cfg)
}

var promHandler = promhttp.Handler()

func serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		promHandler.ServeHTTP(rw, r)
	default:
		proxy.ServeHTTP(rw, r)
	}
}

func listenAndServe(cfg *config.Server) {
	s := &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      http.HandlerFunc(serveHTTP),
		ReadTimeout:  time.Minute,
		WriteTimeout: time.Minute,
		IdleTimeout:  time.Minute * 10,
		ErrorLog:     log.ErrorLogger,
	}

	var ln net.Listener
	var serveInfo string
	if !cfg.IsTLS {
		ln = newListener(cfg.ListenAddr)
		serveInfo = fmt.Sprintf("http on %q", cfg.ListenAddr)
	} else {
		ln = newTLSListener(cfg.ListenAddr, cfg.TLSConfig)
		serveInfo = fmt.Sprintf("https on %q", cfg.ListenAddr)
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

func newTLSListener(laddr string, tlsConf config.TLSConfig) net.Listener {
	tlsConfig := tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
	}

	if len(tlsConf.KeyFile) > 0 && len(tlsConf.CertFile) > 0 {
		cert, err := tls.LoadX509KeyPair(tlsConf.CertFile, tlsConf.KeyFile)
		if err != nil {
			log.Fatalf("cannot load cert for `tls_config.cert_file`=%q, `tls_config.key_file`=%q: %s",
				tlsConf.CertFile, tlsConf.KeyFile, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	} else {
		if len(tlsConf.CertCacheDir) > 0 {
			if _, err := os.Stat(tlsConf.CertCacheDir); os.IsNotExist(err) {
				log.Fatalf("folder %q does not exist", tlsConf.CertCacheDir)
			}
		}

		var hp autocert.HostPolicy
		if len(tlsConf.HostPolicy) != 0 {
			allowedHosts := make(map[string]struct{}, len(tlsConf.HostPolicy))
			for _, v := range tlsConf.HostPolicy {
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
			Cache:      autocert.DirCache(tlsConf.CertCacheDir),
			HostPolicy: hp,
		}

		tlsConfig.GetCertificate = m.GetCertificate
	}

	ln := newListener(laddr)
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

func reloadConfig() (*config.Server, error) {
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		return nil, fmt.Errorf("can't load config %q: %s", *configFile, err)
	}

	if err := proxy.ApplyConfig(cfg); err != nil {
		return nil, err
	}

	networks.Store(&cfg.Networks)
	log.SetDebug(cfg.LogDebug)
	return &cfg.Server, nil
}
