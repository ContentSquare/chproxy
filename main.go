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

var (
	configFile = flag.String("config", "", "Proxy configuration filename")
	version    = flag.Bool("version", false, "Prints current version and exits")
)

var (
	proxy = newReverseProxy()

	allowedNetworksHTTP    atomic.Value
	allowedNetworksHTTPS   atomic.Value
	allowedNetworksMetrics atomic.Value
)

func main() {
	flag.Parse()
	if *version {
		fmt.Printf("%s\n", versionString())
		os.Exit(0)
	}

	log.Infof("%s", versionString())
	log.Infof("Loading config: %s", *configFile)
	cfg, err := reloadConfig()
	if err != nil {
		log.Fatalf("error while loading config: %s", err)
	}
	log.Infof("Loading config %q: successful", *configFile)

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

	if len(cfg.Server.HTTP.ListenAddr) == 0 && len(cfg.Server.HTTPS.ListenAddr) == 0 {
		panic("BUG: broken config validation - `listen_addr` is not configured")
	}

	maxResponseTime := getMaxResponseTime(cfg)
	if len(cfg.Server.HTTPS.ListenAddr) != 0 {
		go serveTLS(cfg.Server.HTTPS, maxResponseTime)
	}
	if len(cfg.Server.HTTP.ListenAddr) != 0 {
		go serve(cfg.Server.HTTP, maxResponseTime)
	}

	select {}
}

// getMaxResponseTime returns the maximum possible response time
// for the given cfg.
func getMaxResponseTime(cfg *config.Config) time.Duration {
	var d time.Duration
	for _, u := range cfg.Users {
		ud := u.MaxExecutionTime + u.MaxQueueTime
		if ud > d {
			d = ud
		}
	}
	for _, c := range cfg.Clusters {
		for _, cu := range c.ClusterUsers {
			cud := cu.MaxExecutionTime + cu.MaxQueueTime
			if cud > d {
				d = cud
			}
		}
	}
	return d
}

func serveTLS(cfg config.HTTPS, maxResponseTime time.Duration) {
	ln, err := net.Listen("tcp4", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", cfg.ListenAddr, err)
	}
	tlsCfg := newTLSConfig(cfg)
	tln := tls.NewListener(ln, tlsCfg)
	log.Infof("Serving https on %q", cfg.ListenAddr)
	if err := listenAndServe(tln, maxResponseTime); err != nil {
		log.Fatalf("TLS server error on %q: %s", cfg.ListenAddr, err)
	}
}

func serve(cfg config.HTTP, maxResponseTime time.Duration) {
	ln, err := net.Listen("tcp4", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("cannot listen for %q: %s", cfg.ListenAddr, err)
	}
	log.Infof("Serving http on %q", cfg.ListenAddr)
	if err := listenAndServe(ln, maxResponseTime); err != nil {
		log.Fatalf("HTTP server error on %q: %s", cfg.ListenAddr, err)
	}
}

func newTLSConfig(cfg config.HTTPS) *tls.Config {
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
	return &tlsCfg
}

func listenAndServe(ln net.Listener, maxResponseTime time.Duration) error {
	if maxResponseTime < 0 {
		maxResponseTime = 0
	}
	// Give an additional minute for the maximum response time,
	// so the response body may be sent to the requester.
	maxResponseTime += time.Minute

	s := &http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		Handler:      http.HandlerFunc(serveHTTP),
		ReadTimeout:  time.Minute,
		WriteTimeout: maxResponseTime,
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
			err := fmt.Errorf("connections to /metrics are not allowed from %s", r.RemoteAddr)
			rw.Header().Set("Connection", "close")
			respondWith(rw, err, http.StatusForbidden)
			return
		}
		promHandler.ServeHTTP(rw, r)
	case "/":
		var err error
		var an *config.Networks
		if r.TLS != nil {
			an = allowedNetworksHTTPS.Load().(*config.Networks)
			err = fmt.Errorf("https connections are not allowed from %s", r.RemoteAddr)
		} else {
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
		err := fmt.Sprintf("Unsupported path: %s", r.URL.Path)
		log.Debugf(err)
		rw.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(rw, err)
	}
}

func reloadConfig() (*config.Config, error) {
	if *configFile == "" {
		log.Fatalf("Missing -config flag")
	}
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
	log.Infof("Loaded config: \n%s", cfg)
	return cfg, nil
}

func versionString() string {
	ver := buildTag
	if len(ver) == 0 {
		ver = "unknown"
	}
	return fmt.Sprintf("chproxy ver. %s, rev. %s, built at %s", ver, buildRevision, buildTime)
}

var (
	buildTag      = "unknown"
	buildRevision = "unknown"
	buildTime     = "unknown"
)
