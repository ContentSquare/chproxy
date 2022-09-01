package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/contentsquare/chproxy/config"
)

type heartBeat struct {
	interval time.Duration
	timeout  time.Duration
	request  string
	response string
	user     string
	password string
}

func newHeartBeat(c config.HeartBeat, firstClusterUser config.ClusterUser) *heartBeat {
	newHB := &heartBeat{
		interval: time.Duration(c.Interval),
		timeout:  time.Duration(c.Timeout),
		request:  c.Request,
		response: c.Response,
	}
	if len(c.Name) > 0 {
		newHB.user = c.Name
		newHB.password = c.Password
	} else {
		newHB.user = firstClusterUser.Name
		newHB.password = firstClusterUser.Password
	}
	return newHB
}

func (hb *heartBeat) isHealthy(addr string) error {
	req, err := http.NewRequest("GET", addr+hb.request, nil)
	if err != nil {
		return err
	}
	if hb.request != "/ping" && hb.user != "" {
		req.SetBasicAuth(hb.user, hb.password)
	}
	ctx, cancel := context.WithTimeout(context.Background(), hb.timeout)
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
		return fmt.Errorf("unexpected response: %s", r)
	}
	return nil
}
