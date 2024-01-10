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
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
)

var (
	configFile = flag.String("config", "", "Proxy configuration filename")
	version    = flag.Bool("version", false, "Prints current version and exits")
	enableTCP6 = flag.Bool("enableTCP6", false, "Whether to enable listening for IPv6 TCP ports. "+
		"By default only IPv4 TCP ports are listened")
)

var (
	proxy *reverseProxy

	// networks allow lists
	allowedNetworksHTTP    atomic.Value
	allowedNetworksHTTPS   atomic.Value
	allowedNetworksMetrics atomic.Value
	proxyHandler           atomic.Value
)

func main() {
	flag.Parse()
	if *version {
		fmt.Printf("%s\n", versionString())
		os.Exit(0)
	}

	log.Infof("%s", versionString())
	log.Infof("Loading config: %s", *configFile)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("error while loading config: %s", err)
	}
	registerMetrics(cfg)
	if err = applyConfig(cfg); err != nil {
		log.Fatalf("error while applying config: %s", err)
	}
	configSuccess.Set(1)
	configSuccessTime.Set(float64(time.Now().Unix()))
	log.Infof("Loading config %q: successful", *configFile)

	setupReloadConfigWatch()

	server := cfg.Server
	if len(server.HTTP.ListenAddr) == 0 && len(server.HTTPS.ListenAddr) == 0 {
		panic("BUG: broken config validation - `listen_addr` is not configured")
	}

	if server.HTTP.ForceAutocertHandler {
		autocertManager = newAutocertManager(server.HTTPS.Autocert)
	}

	notifyReady()

	if len(server.HTTPS.ListenAddr) != 0 {
		go serveTLS(server.HTTPS)
	}
	if len(server.HTTP.ListenAddr) != 0 {
		go serve(server.HTTP)
	}

	select {}
}

func notifyReady() {
	sent, err := sdNotifyReady()
	if err != nil {
		log.Errorf("SdNotify error: %s", err)
	} else if !sent {
		log.Debugf("SdNotify unsupported (not a systemd service?)")
	}
}

func setupReloadConfigWatch() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		for {
			if <-c == syscall.SIGHUP {
				log.Infof("SIGHUP received. Going to reload config %s ...", *configFile)
				if err := reloadConfig(); err != nil {
					log.Errorf("error while reloading config: %s", err)
					continue
				}
				log.Infof("Reloading config %s: successful", *configFile)
			}
		}
	}()
}

var autocertManager *autocert.Manager

func newAutocertManager(cfg config.Autocert) *autocert.Manager {
	if len(cfg.CacheDir) > 0 {
		if err := os.MkdirAll(cfg.CacheDir, 0o700); err != nil {
			log.Fatalf("error while creating folder %q: %s", cfg.CacheDir, err)
		}
	}
	var hp autocert.HostPolicy
	if len(cfg.AllowedHosts) != 0 {
		allowedHosts := make(map[string]struct{}, len(cfg.AllowedHosts))
		for _, v := range cfg.AllowedHosts {
			allowedHosts[v] = struct{}{}
		}
		hp = func(_ context.Context, host string) error {
			if _, ok := allowedHosts[host]; ok {
				return nil
			}
			return fmt.Errorf("host %q doesn't match `host_policy` configuration", host)
		}
	}
	return &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(cfg.CacheDir),
		HostPolicy: hp,
	}
}

func newListener(listenAddr string) net.Listener {
	network := "tcp4"
	if *enableTCP6 {
		// Enable listening on both tcp4 and tcp6
		network = "tcp"
	}
	ln, err := net.Listen(network, listenAddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", listenAddr, err)
	}
	return ln
}

func serveTLS(cfg config.HTTPS) {
	ln := newListener(cfg.ListenAddr)

	h := http.HandlerFunc(serveHTTP)

	tlsCfg, err := cfg.TLS.BuildTLSConfig(autocertManager)
	if err != nil {
		log.Fatalf("cannot build TLS config: %s", err)
	}
	tln := tls.NewListener(ln, tlsCfg)
	log.Infof("Serving https on %q", cfg.ListenAddr)
	if err := listenAndServe(tln, h, cfg.TimeoutCfg); err != nil {
		log.Fatalf("TLS server error on %q: %s", cfg.ListenAddr, err)
	}
}

func serve(cfg config.HTTP) {
	var h http.Handler
	ln := newListener(cfg.ListenAddr)

	h = http.HandlerFunc(serveHTTP)
	if cfg.ForceAutocertHandler {
		if autocertManager == nil {
			panic("BUG: autocertManager is not inited")
		}
		addr := ln.Addr().String()
		parts := strings.Split(addr, ":")
		if parts[len(parts)-1] != "80" {
			log.Fatalf("`letsencrypt` specification requires http server to listen on :80 port to satisfy http-01 challenge. " +
				"Otherwise, certificates will be impossible to generate")
		}
		h = autocertManager.HTTPHandler(h)
	}
	log.Infof("Serving http on %q", cfg.ListenAddr)
	if err := listenAndServe(ln, h, cfg.TimeoutCfg); err != nil {
		log.Fatalf("HTTP server error on %q: %s", cfg.ListenAddr, err)
	}
}

func newServer(ln net.Listener, h http.Handler, cfg config.TimeoutCfg) *http.Server {
	// nolint:gosec // We already configured ReadTimeout, so no need to set ReadHeaderTimeout as well.
	return &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      h,
		ReadTimeout:  time.Duration(cfg.ReadTimeout),
		WriteTimeout: time.Duration(cfg.WriteTimeout),
		IdleTimeout:  time.Duration(cfg.IdleTimeout),
		// Suppress error logging from the server, since chproxy
		// must handle all these errors in the code.
		ErrorLog: log.NilLogger,
	}
}

func listenAndServe(ln net.Listener, h http.Handler, cfg config.TimeoutCfg) error {
	s := newServer(ln, h, cfg)
	return s.Serve(ln)
}

var promHandler = promhttp.Handler()

//nolint:cyclop //TODO reduce complexity here.
func serveHTTP(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		// Only GET and POST methods are supported.
	case http.MethodOptions:
		// This is required for CORS shit :)
		rw.Header().Set("Allow", "GET,POST")
		return
	default:
		err := fmt.Errorf("%q: unsupported method %q", r.RemoteAddr, r.Method)
		rw.Header().Set("Connection", "close")
		respondWith(rw, err, http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/favicon.ico":
	case "/metrics":
		// nolint:forcetypeassert // We will cover this by tests as we control what is stored.
		an := allowedNetworksMetrics.Load().(*config.Networks)
		if !an.Contains(r.RemoteAddr) {
			err := fmt.Errorf("connections to /metrics are not allowed from %s", r.RemoteAddr)
			rw.Header().Set("Connection", "close")
			respondWith(rw, err, http.StatusForbidden)
			return
		}
		proxy.refreshCacheMetrics()
		promHandler.ServeHTTP(rw, r)
	case "/", "/query":
		var err error
		// nolint:forcetypeassert // We will cover this by tests as we control what is stored.
		proxyHandler := proxyHandler.Load().(*ProxyHandler)
		r.RemoteAddr = proxyHandler.GetRemoteAddr(r)

		var an *config.Networks
		if r.TLS != nil {
			// nolint:forcetypeassert // We will cover this by tests as we control what is stored.
			an = allowedNetworksHTTPS.Load().(*config.Networks)
			err = fmt.Errorf("https connections are not allowed from %s", r.RemoteAddr)
		} else {
			// nolint:forcetypeassert // We will cover this by tests as we control what is stored.
			an = allowedNetworksHTTP.Load().(*config.Networks)
			err = fmt.Errorf("http connections are not allowed from %s", r.RemoteAddr)
		}
		if !an.Contains(r.RemoteAddr) {
			rw.Header().Set("Connection", "close")
			respondWith(rw, err, http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(rw, r)
	default:
		badRequest.Inc()
		err := fmt.Errorf("%q: unsupported path: %q", r.RemoteAddr, r.URL.Path)
		rw.Header().Set("Connection", "close")
		respondWith(rw, err, http.StatusBadRequest)
	}
}

func loadConfig() (*config.Config, error) {
	if *configFile == "" {
		log.Fatalf("Missing -config flag")
	}
	cfg, err := config.LoadFile(*configFile)
	if err != nil {
		return nil, fmt.Errorf("can't load config %q: %w", *configFile, err)
	}
	return cfg, nil
}

// a configuration parameter value that is used in proxy initialization
// changed
func proxyConfigChanged(cfgCp *config.ConnectionPool, rp *reverseProxy) bool {
	return cfgCp.MaxIdleConns != proxy.maxIdleConns ||
		cfgCp.MaxIdleConnsPerHost != proxy.maxIdleConnsPerHost
}

func applyConfig(cfg *config.Config) error {
	if proxy == nil || proxyConfigChanged(&cfg.ConnectionPool, proxy) {
		proxy = newReverseProxy(&cfg.ConnectionPool)
	}
	if err := proxy.applyConfig(cfg); err != nil {
		return err
	}
	allowedNetworksHTTP.Store(&cfg.Server.HTTP.AllowedNetworks)
	allowedNetworksHTTPS.Store(&cfg.Server.HTTPS.AllowedNetworks)
	allowedNetworksMetrics.Store(&cfg.Server.Metrics.AllowedNetworks)
	proxyHandler.Store(NewProxyHandler(&cfg.Server.Proxy))
	log.SetDebug(cfg.LogDebug)
	log.Infof("Loaded config:\n%s", cfg)

	return nil
}

func reloadConfig() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return applyConfig(cfg)
}

var (
	buildTag      = "unknown"
	buildRevision = "unknown"
	buildTime     = "unknown"
)

func versionString() string {
	ver := buildTag
	if len(ver) == 0 {
		ver = "unknown"
	}
	return fmt.Sprintf("chproxy ver. %s, rev. %s, built at %s", ver, buildRevision, buildTime)
}
