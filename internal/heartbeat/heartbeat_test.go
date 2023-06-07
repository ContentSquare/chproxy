package heartbeat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/stretchr/testify/assert"
)

var (
	hbHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			if _, _, found := r.BasicAuth(); !found {
				fmt.Fprintln(w, "Ok.")
			} else {
				fmt.Fprintln(w, "User is not expected.")
			}
			return
		}

		if r.URL.Path == "/timeout" {
			time.Sleep(2 * time.Second)
			fmt.Fprintln(w, "Ok.")
			return
		}

		if r.URL.Path == "/" && r.URL.Query().Get("query") == "SELECT 1" {
			if _, _, found := r.BasicAuth(); found {
				fmt.Fprintln(w, "Ok.")
			} else {
				fmt.Fprintln(w, "User is required.")
			}
			return
		}

		qid := r.URL.Query().Get("query")
		if len(qid) != 0 {
			fmt.Fprintln(w, "1")
			return
		}
		fmt.Fprintln(w, "wrong")
	})

	fakeHBServer = httptest.NewServer(hbHandler)

	heartBeatFullCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(30 * time.Second),
		Request:  "/?query=SELECT%201",
		Response: "Ok.\n",
	}

	heartBeatDefaultCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(30 * time.Second),
		Request:  "/ping",
		Response: "Ok.\n",
	}

	heartBeatTimeoutCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(1 * time.Second),
		Request:  "/timeout",
		Response: "Ok.\n",
	}

	heartBeatWrongResponseCfg = config.HeartBeat{
		Interval: config.Duration(20 * time.Second),
		Timeout:  config.Duration(30 * time.Second),
		Request:  "/wrongQuery",
		Response: "Ok.\n",
	}
)

func TestNewHeartBeat(t *testing.T) {
	tests := []struct {
		name          string
		cfg           config.HeartBeat
		opts          []Option
		expectedError error
	}{
		{
			name:          "Use default user when not calling the DefaultEndpoint",
			cfg:           heartBeatFullCfg,
			opts:          []Option{WithDefaultUser("web", "123")},
			expectedError: nil,
		},
		{
			name:          "Healthcheck on a non-existing endpoint response on healthcheck",
			cfg:           heartBeatWrongResponseCfg,
			opts:          []Option{WithDefaultUser("web", "123")},
			expectedError: errUnexpectedResponse,
		},
		{
			name:          "No basic auth needed when calling the DefaultEndpoint",
			cfg:           heartBeatDefaultCfg,
			opts:          []Option{},
			expectedError: nil,
		},
		{
			name:          "Timeout on healthcheck",
			cfg:           heartBeatTimeoutCfg,
			opts:          []Option{WithDefaultUser("web", "123")},
			expectedError: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hb := NewHeartbeat(tt.cfg, tt.opts...)
			err := hb.IsHealthy(context.TODO(), fakeHBServer.URL)
			if tt.expectedError != nil {
				assert.ErrorIs(t, err, tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
