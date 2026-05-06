package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/acme/autocert"
)

const pingEndpoint string = "/ping"

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
	allowPing              atomic.Bool

	// gracefulShutdownTimeout stores the configured shutdown timeout
	gracefulShutdownTimeout time.Duration

	// activeConnections tracks the number of currently active HTTP connections
	activeConnections atomic.Int64
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
	if err = log.InitReplacer(cfg.LogMasks); err != nil {
		log.Fatalf("error while log replacer init: %s", err)
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

	gracefulShutdownTimeout = getGracefulShutdownTimeout(server.GracefulShutdownTimeout)
	log.Infof("Graceful shutdown timeout: %s", gracefulShutdownTimeout)

	notifyReady()

	var httpServer, httpsServer *http.Server
	serverErrors := make(chan error, 2)

	if len(server.HTTPS.ListenAddr) != 0 {
		httpsServer = startTLSServer(server.HTTPS, serverErrors)
	}
	if len(server.HTTP.ListenAddr) != 0 {
		httpServer = startHTTPServer(server.HTTP, serverErrors)
	}

	if err := waitForShutdownSignal(httpServer, httpsServer, serverErrors); err != nil {
		log.Errorf("Shutdown error: %s", err)
		os.Exit(1)
	}
}

// getGracefulShutdownTimeout returns the graceful shutdown timeout from config.
func getGracefulShutdownTimeout(configTimeout config.Duration) time.Duration {
	const defaultTimeout = 25 * time.Second

	if configTimeout > 0 {
		return time.Duration(configTimeout)
	}
	return defaultTimeout
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

// waitForShutdownSignal waits for SIGTERM or SIGINT and performs graceful shutdown
func waitForShutdownSignal(httpServer, httpsServer *http.Server, serverErrors <-chan error) error {
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case <-sigCtx.Done():
		log.Infof("Shutdown signal received")
		return gracefulShutdown(httpServer, httpsServer, serverErrors)
	}

	return nil
}

// gracefulShutdown performs graceful shutdown of HTTP servers
func gracefulShutdown(httpServer, httpsServer *http.Server, serverErrors <-chan error) error {
	initialConns := activeConnections.Load()
	log.Infof("Starting graceful shutdown with %d open connections", initialConns)

	// Create shutdown deadline
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	go func() {
		for {
			select {
			case err, ok := <-serverErrors:
				if !ok {
					return
				}
				if err != nil {
					log.Errorf("Server error during shutdown: %s", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Shutdown servers concurrently
	var wg sync.WaitGroup
	var httpErr, httpsErr error

	shutdownServer := func(s *http.Server, label string, errp *error) {
		if s == nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Infof("Shutting down %s server...", label)
			if err := s.Shutdown(ctx); err != nil {
				*errp = fmt.Errorf("%s shutdown: %w", label, err)
			} else {
				log.Infof("%s server stopped", label)
			}
		}()
	}
	shutdownServer(httpServer, "HTTP", &httpErr)
	shutdownServer(httpsServer, "HTTPS", &httpsErr)

	// Signal channel for shutdown completion
	shutdownComplete := make(chan struct{})
	go func() {
		wg.Wait()

		// Clean up proxy resources
		if proxy != nil {
			log.Infof("Closing proxy resources...")
			if err := proxy.close(); err != nil {
				log.Errorf("Proxy close error: %s", err)
			}
		}
		close(shutdownComplete)
	}()

	// Wait for shutdown to complete or timeout
	select {
	case <-shutdownComplete:
		finalConns := activeConnections.Load()
		joinedErrs := errors.Join(httpErr, httpsErr)

		if joinedErrs != nil {
			return fmt.Errorf("shutdown completed with errors (remaining open connections: %d): %w", finalConns, joinedErrs)
		}
		if finalConns > 0 {
			log.Errorf("Graceful shutdown completed with %d open connections still active", finalConns)
		} else if initialConns > 0 {
			log.Infof("Graceful shutdown completed successfully (all connections closed)")
		} else {
			log.Infof("Graceful shutdown completed successfully")
		}
		return nil
	case <-ctx.Done():
		remainingConns := activeConnections.Load()
		return fmt.Errorf("shutdown timeout exceeded with %d open connections still active", remainingConns)
	}

	return nil
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

// startTLSServer starts the HTTPS server and returns the server instance for graceful shutdown
func startTLSServer(cfg config.HTTPS, serverErrors chan<- error) *http.Server {
	ln := newListener(cfg.ListenAddr)

	h := http.HandlerFunc(serveHTTP)

	tlsCfg, err := cfg.TLS.BuildTLSConfig(autocertManager)
	if err != nil {
		log.Fatalf("cannot build TLS config: %s", err)
	}
	tln := tls.NewListener(ln, tlsCfg)

	server := newServer(tln, h, cfg.TimeoutCfg)
	log.Infof("Serving https on %q", cfg.ListenAddr)

	go func() {
		if err := server.Serve(tln); err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("TLS server error on %q: %w", cfg.ListenAddr, err)
		}
	}()

	return server
}

// startHTTPServer starts the HTTP server and returns the server instance for graceful shutdown
func startHTTPServer(cfg config.HTTP, serverErrors chan<- error) *http.Server {
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

	server := newServer(ln, h, cfg.TimeoutCfg)
	log.Infof("Serving http on %q", cfg.ListenAddr)

	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("HTTP server error on %q: %w", cfg.ListenAddr, err)
		}
	}()

	return server
}

func newServer(ln net.Listener, h http.Handler, cfg config.TimeoutCfg) *http.Server {
	// nolint:gosec // We already configured ReadTimeout, so no need to set ReadHeaderTimeout as well.
	return &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      h,
		ReadTimeout:  time.Duration(cfg.ReadTimeout),
		WriteTimeout: time.Duration(cfg.WriteTimeout),
		IdleTimeout:  time.Duration(cfg.IdleTimeout),
		ConnState: func(conn net.Conn, state http.ConnState) {
			switch state {
			case http.StateNew:
				activeConnections.Add(1)
				log.Debugf("Connection opened from %s (active: %d)", conn.RemoteAddr(), activeConnections.Load())
			case http.StateClosed, http.StateHijacked:
				activeConnections.Add(-1)
				log.Debugf("Connection closed from %s (active: %d)", conn.RemoteAddr(), activeConnections.Load())
			}
		},
		// Suppress error logging from the server, since chproxy
		// must handle all these errors in the code.
		ErrorLog: log.NilLogger,
	}
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
	case "/", "/query", pingEndpoint:
		var err error

		if r.URL.Path == pingEndpoint && !allowPing.Load() {
			err = fmt.Errorf("ping is not allowed")
			respondWith(rw, err, http.StatusForbidden)
			return
		}

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
	allowPing.Store(cfg.AllowPing)
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
