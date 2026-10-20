package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/prometheus/client_golang/prometheus"
)

const trafficCloneResponseDrainLimit = 64 << 10

type trafficCloner struct {
	client         *http.Client
	tempDir        string
	maxCaptureSize int64
	captureTokens  chan struct{}
	cloneTokens    chan struct{}
	targets        []*trafficCloneTarget
}

type trafficCloneTarget struct {
	name            string
	fromCluster     string
	fromUser        string
	fromClusterUser string
	cluster         *cluster
	clusterUser     *clusterUser
	percent         float64
	timeout         time.Duration
}

// cloneSource holds a snapshot of the originating request's identity.
// It is created once from a *scope before goroutines are launched so that
// goroutines do not hold a reference to the scope past the primary request
// lifecycle (which could produce misleading in-flight counters on s.dec()).
type cloneSource struct {
	user        *user
	userName    string
	clusterName string
	cuName      string
}

func newCloneSource(s *scope) cloneSource {
	return cloneSource{
		user:        s.user,
		userName:    s.user.name,
		clusterName: s.cluster.name,
		cuName:      s.clusterUser.name,
	}
}

func newTrafficCloner(cfg config.TrafficCloning, clusters map[string]*cluster, transport http.RoundTripper) (*trafficCloner, error) {
	if !cfg.IsEnabled() {
		return nil, nil
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("`traffic_cloning.targets` must contain at least one target when traffic cloning is enabled")
	}
	if err := prepareTrafficCloneTempDir(cfg.TempDir); err != nil {
		return nil, err
	}

	targets := make([]*trafficCloneTarget, 0, len(cfg.Targets))
	for _, tc := range cfg.Targets {
		if tc.Name == "" {
			return nil, fmt.Errorf("`traffic_cloning.target.name` cannot be empty")
		}
		c := clusters[tc.ToCluster]
		if c == nil {
			return nil, fmt.Errorf("unknown `traffic_cloning.target.to_cluster` %q", tc.ToCluster)
		}

		cu := c.users[tc.ToUser]
		if cu == nil {
			return nil, fmt.Errorf("unknown `traffic_cloning.target.to_user` %q in cluster %q", tc.ToUser, tc.ToCluster)
		}
		if tc.Percent < 0 || tc.Percent > 100 {
			return nil, fmt.Errorf("`traffic_cloning.target.percent` must be between 0 and 100 for %q", tc.Name)
		}

		targets = append(targets, &trafficCloneTarget{
			name:            tc.Name,
			fromCluster:     tc.FromCluster,
			fromUser:        tc.FromUser,
			fromClusterUser: tc.FromClusterUser,
			cluster:         c,
			clusterUser:     cu,
			percent:         tc.Percent,
			timeout:         time.Duration(tc.Timeout),
		})
	}

	var captureTokens chan struct{}
	if cfg.MaxConcurrentCaptures > 0 {
		captureTokens = make(chan struct{}, cfg.MaxConcurrentCaptures)
	}

	var cloneTokens chan struct{}
	if cfg.MaxConcurrentClones > 0 {
		cloneTokens = make(chan struct{}, cfg.MaxConcurrentClones)
	}

	return &trafficCloner{
		client: &http.Client{
			Transport: transport,
		},
		tempDir:        cfg.TempDir,
		maxCaptureSize: int64(cfg.MaxCaptureSize),
		captureTokens:  captureTokens,
		cloneTokens:    cloneTokens,
		targets:        targets,
	}, nil
}

func prepareTrafficCloneTempDir(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cannot create `traffic_cloning.temp_dir` %q: %w", dir, err)
	}

	// Remove stale captures left by a previous crash or unclean shutdown.
	stale, _ := filepath.Glob(filepath.Join(dir, "chproxy-traffic-clone-*"))
	for _, f := range stale {
		_ = os.Remove(f)
	}

	f, err := os.CreateTemp(dir, "chproxy-traffic-clone-probe-*")
	if err != nil {
		return fmt.Errorf("cannot write to `traffic_cloning.temp_dir` %q: %w", dir, err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot close probe file in `traffic_cloning.temp_dir` %q: %w", dir, err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("cannot remove probe file from `traffic_cloning.temp_dir` %q: %w", dir, err)
	}
	return nil
}

func (tc *trafficCloner) cloneRequest(source *scope, req *http.Request) {
	if tc == nil {
		return
	}

	targets := tc.pickTargets(source)
	if len(targets) == 0 {
		return
	}

	if req.Body != nil && req.Body != http.NoBody {
		if tc.maxCaptureSize > 0 && req.ContentLength > tc.maxCaptureSize {
			tc.skipTargets(source, targets, "request body is larger than traffic_cloning.max_capture_size")
			return
		}
		if !tc.tryAcquireCapture() {
			tc.skipTargets(source, targets, "traffic clone capture concurrency limit reached")
			return
		}
		capture, err := newTrafficCloneBodyCapture(req.Body, tc, source, targets, req)
		if err != nil {
			tc.releaseCapture()
			src := newCloneSource(source)
			for _, target := range targets {
				labels := trafficCloneLabels(src, target)
				trafficCloneRequests.With(trafficCloneResultLabels(labels, "error")).Inc()
				log.Errorf("%s: cannot initialize traffic clone capture for target %q: %s", src.userName, target.name, err)
			}
			return
		}
		req.Body = capture
		return
	}

	tc.cloneRequestsWithoutBody(source, targets, req)
}

func (tc *trafficCloner) tryAcquireCapture() bool {
	if tc.captureTokens == nil {
		return true
	}
	select {
	case tc.captureTokens <- struct{}{}:
		return true
	default:
		return false
	}
}

func (tc *trafficCloner) releaseCapture() {
	if tc.captureTokens == nil {
		return
	}
	<-tc.captureTokens
}

func (tc *trafficCloner) tryAcquireClone() bool {
	if tc.cloneTokens == nil {
		return true
	}
	select {
	case tc.cloneTokens <- struct{}{}:
		return true
	default:
		return false
	}
}

func (tc *trafficCloner) releaseClone() {
	if tc.cloneTokens == nil {
		return
	}
	<-tc.cloneTokens
}

func (tc *trafficCloner) skipTargets(source *scope, targets []*trafficCloneTarget, reason string) {
	src := newCloneSource(source)
	for _, target := range targets {
		labels := trafficCloneLabels(src, target)
		trafficCloneRequests.With(trafficCloneResultLabels(labels, "skipped")).Inc()
		log.Debugf("%s: traffic clone to target %q skipped: %s", src.userName, target.name, reason)
	}
}

func (tc *trafficCloner) pickTargets(source *scope) []*trafficCloneTarget {
	targets := make([]*trafficCloneTarget, 0, len(tc.targets))
	for _, target := range tc.targets {
		if target.matches(source) && (target.percent >= 100 || tc.randomPercent() < target.percent) {
			targets = append(targets, target)
		}
	}
	return targets
}

func (target *trafficCloneTarget) matches(source *scope) bool {
	if target.fromCluster != "" && target.fromCluster != source.cluster.name {
		return false
	}
	if target.fromUser != "" && target.fromUser != source.user.name {
		return false
	}
	if target.fromClusterUser != "" && target.fromClusterUser != source.clusterUser.name {
		return false
	}
	return true
}

func (tc *trafficCloner) randomPercent() float64 {
	return rand.Float64() * 100
}

func (tc *trafficCloner) cloneRequestsWithoutBody(source *scope, targets []*trafficCloneTarget, req *http.Request) {
	src := newCloneSource(source)
	for _, target := range targets {
		if !tc.tryAcquireClone() {
			labels := trafficCloneLabels(src, target)
			trafficCloneRequests.With(trafficCloneResultLabels(labels, "skipped")).Inc()
			log.Debugf("%s: traffic clone to target %q skipped: clone concurrency limit reached", src.userName, target.name)
			continue
		}
		cloneReq := req.Clone(context.Background())
		cloneReq.RequestURI = ""
		go func() {
			defer tc.releaseClone()
			tc.send(src, target, cloneReq)
		}()
	}
}

func (tc *trafficCloner) cloneCapturedRequests(source cloneSource, targets []*trafficCloneTarget, requests []*http.Request, fileName string) {
	defer tc.releaseCapture()

	var wg sync.WaitGroup
	for i, target := range targets {
		target := target
		cloneReq := requests[i]

		if !tc.tryAcquireClone() {
			labels := trafficCloneLabels(source, target)
			trafficCloneRequests.With(trafficCloneResultLabels(labels, "skipped")).Inc()
			log.Debugf("%s: traffic clone to target %q skipped: clone concurrency limit reached", source.userName, target.name)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer tc.releaseClone()

			f, err := os.Open(fileName)
			if err != nil {
				labels := trafficCloneLabels(source, target)
				trafficCloneRequests.With(trafficCloneResultLabels(labels, "error")).Inc()
				log.Errorf("%s: cannot open traffic clone capture for target %q: %s", source.userName, target.name, err)
				return
			}
			defer f.Close()
			cloneReq.Body = f
			tc.send(source, target, cloneReq)
		}()
	}
	wg.Wait()
	if err := os.Remove(fileName); err != nil {
		log.Errorf("cannot remove traffic clone capture %q: %s", fileName, err)
	}
}

func (tc *trafficCloner) send(source cloneSource, target *trafficCloneTarget, req *http.Request) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if target.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, target.timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	req = req.WithContext(ctx)
	cloneScope := newScope(req, source.user, target.cluster, target.clusterUser, "", getSessionTimeout(req))
	cloneScope.decorateRequest(req)

	labels := trafficCloneLabels(source, target)
	trafficCloneInFlight.With(labels).Inc()
	defer trafficCloneInFlight.With(labels).Dec()

	startTime := time.Now()
	resp, err := tc.client.Do(req)
	trafficCloneDuration.With(labels).Observe(time.Since(startTime).Seconds())
	if err != nil {
		result := "error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result = "canceled"
		}
		trafficCloneRequests.With(trafficCloneResultLabels(labels, result)).Inc()
		log.Debugf("%s: traffic clone to target %q failed: %s", source.userName, target.name, err)
		return
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, trafficCloneResponseDrainLimit))

	trafficCloneRequests.With(trafficCloneResultLabels(labels, "success")).Inc()
	trafficCloneStatusCodes.With(trafficCloneStatusCodeLabels(labels, resp.StatusCode)).Inc()
	log.Debugf("%s: traffic clone to target %q completed with status code %d", source.userName, target.name, resp.StatusCode)
}

func trafficCloneLabels(source cloneSource, target *trafficCloneTarget) prometheus.Labels {
	return prometheus.Labels{
		"user":                source.userName,
		"cluster":             source.clusterName,
		"cluster_user":        source.cuName,
		"target":              target.name,
		"target_cluster":      target.cluster.name,
		"target_cluster_user": target.clusterUser.name,
	}
}

func trafficCloneResultLabels(labels prometheus.Labels, result string) prometheus.Labels {
	withResult := prometheus.Labels{}
	for k, v := range labels {
		withResult[k] = v
	}
	withResult["result"] = result
	return withResult
}

func trafficCloneStatusCodeLabels(labels prometheus.Labels, code int) prometheus.Labels {
	withCode := prometheus.Labels{}
	for k, v := range labels {
		withCode[k] = v
	}
	withCode["code"] = strconv.Itoa(code)
	return withCode
}

type trafficCloneBodyCapture struct {
	io.ReadCloser

	cloner  *trafficCloner
	source  cloneSource
	targets []*trafficCloneTarget

	file     *os.File
	fileName string
	requests []*http.Request
	captured int64

	done      sync.Once
	finalized atomic.Bool
}

func newTrafficCloneBodyCapture(
	body io.ReadCloser,
	cloner *trafficCloner,
	source *scope,
	targets []*trafficCloneTarget,
	req *http.Request,
) (*trafficCloneBodyCapture, error) {
	f, err := os.CreateTemp(cloner.tempDir, "chproxy-traffic-clone-*")
	if err != nil {
		return nil, err
	}

	requests := make([]*http.Request, len(targets))
	for i := range targets {
		cloneReq := req.Clone(context.Background())
		cloneReq.RequestURI = ""
		cloneReq.Body = nil
		requests[i] = cloneReq
	}

	return &trafficCloneBodyCapture{
		ReadCloser: body,
		cloner:     cloner,
		source:     newCloneSource(source),
		targets:    targets,
		file:       f,
		fileName:   f.Name(),
		requests:   requests,
	}, nil
}

func (c *trafficCloneBodyCapture) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if c.finalized.Load() {
		return n, err
	}
	if n > 0 {
		c.captured += int64(n)
		if c.cloner.maxCaptureSize > 0 && c.captured > c.cloner.maxCaptureSize {
			c.skip("request body exceeded traffic_cloning.max_capture_size")
			return n, err
		}
		if _, werr := c.file.Write(p[:n]); werr != nil {
			c.fail(werr)
		}
	}
	if err == io.EOF {
		c.finish()
	} else if err != nil {
		c.abort()
	}
	return n, err
}

func (c *trafficCloneBodyCapture) Close() error {
	c.abort()
	return c.ReadCloser.Close()
}

func (c *trafficCloneBodyCapture) finish() {
	c.done.Do(func() {
		c.finalized.Store(true)
		if err := c.file.Close(); err != nil {
			_ = os.Remove(c.fileName)
			for _, target := range c.targets {
				labels := trafficCloneLabels(c.source, target)
				trafficCloneRequests.With(trafficCloneResultLabels(labels, "error")).Inc()
				log.Errorf("%s: cannot close traffic clone capture for target %q: %s", c.source.userName, target.name, err)
			}
			c.cloner.releaseCapture()
			return
		}
		go c.cloner.cloneCapturedRequests(c.source, c.targets, c.requests, c.fileName)
	})
}

func (c *trafficCloneBodyCapture) abort() {
	c.done.Do(func() {
		c.finalized.Store(true)
		_ = c.file.Close()
		_ = os.Remove(c.fileName)
		c.cloner.releaseCapture()
	})
}

func (c *trafficCloneBodyCapture) fail(err error) {
	c.done.Do(func() {
		c.finalized.Store(true)
		_ = c.file.Close()
		_ = os.Remove(c.fileName)
		for _, target := range c.targets {
			labels := trafficCloneLabels(c.source, target)
			trafficCloneRequests.With(trafficCloneResultLabels(labels, "error")).Inc()
			log.Errorf("%s: cannot write traffic clone capture for target %q: %s", c.source.userName, target.name, err)
		}
		c.cloner.releaseCapture()
	})
}

func (c *trafficCloneBodyCapture) skip(reason string) {
	c.done.Do(func() {
		c.finalized.Store(true)
		_ = c.file.Close()
		_ = os.Remove(c.fileName)
		for _, target := range c.targets {
			labels := trafficCloneLabels(c.source, target)
			trafficCloneRequests.With(trafficCloneResultLabels(labels, "skipped")).Inc()
			log.Debugf("%s: traffic clone to target %q skipped: %s", c.source.userName, target.name, reason)
		}
		c.cloner.releaseCapture()
	})
}
