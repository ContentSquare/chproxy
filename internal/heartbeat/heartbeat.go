package heartbeat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/contentsquare/chproxy/config"
)

var errUnexpectedResponse = fmt.Errorf("unexpected response")

type HeartBeat interface {
	IsHealthy(ctx context.Context, addr string) error
	Interval() time.Duration
}

type heartBeatOpts struct {
	defaultUser     string
	defaultPassword string
}

type Option interface {
	apply(*heartBeatOpts)
}

type defaultUser struct {
	defaultUser     string
	defaultPassword string
}

func (o defaultUser) apply(opts *heartBeatOpts) {
	opts.defaultUser = o.defaultUser
	opts.defaultPassword = o.defaultPassword
}

func WithDefaultUser(user, password string) Option {
	return defaultUser{
		defaultUser:     user,
		defaultPassword: password,
	}
}

type heartBeat struct {
	interval time.Duration
	timeout  time.Duration
	request  string
	response string
	user     string
	password string
}

// User credentials are not needed
const defaultEndpoint string = "/ping"

func NewHeartbeat(c config.HeartBeat, options ...Option) HeartBeat {
	opts := &heartBeatOpts{}
	for _, o := range options {
		o.apply(opts)
	}

	newHB := &heartBeat{
		interval: time.Duration(c.Interval),
		timeout:  time.Duration(c.Timeout),
		request:  c.Request,
		response: c.Response,
	}

	if c.Request != defaultEndpoint {
		if c.User != "" {
			newHB.user = c.User
			newHB.password = c.Password
		} else {
			newHB.user = opts.defaultUser
			newHB.password = opts.defaultPassword
		}
	}

	if newHB.request != defaultEndpoint && newHB.user == "" {
		panic("BUG: user is empty, no default user provided")
	}

	return newHB
}

func (hb *heartBeat) IsHealthy(ctx context.Context, addr string) error {
	req, err := http.NewRequest("GET", addr+hb.request, nil)
	if err != nil {
		return err
	}
	if hb.request != defaultEndpoint {
		req.SetBasicAuth(hb.user, hb.password)
	}
	ctx, cancel := context.WithTimeout(ctx, hb.timeout)
	defer cancel()
	req = req.WithContext(ctx)

	startTime := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot send request in %s: %w", time.Since(startTime), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-200 status code: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cannot read response in %s: %w", time.Since(startTime), err)
	}
	r := string(body)
	if r != hb.response {
		return fmt.Errorf("%w: %s", errUnexpectedResponse, r)
	}
	return nil
}

func (hb *heartBeat) Interval() time.Duration {
	return hb.interval
}
