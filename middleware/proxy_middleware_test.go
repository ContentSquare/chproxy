package middleware

import (
	"net/http"
	"testing"

	"github.com/contentsquare/chproxy/config"
)

type testHandler struct {
	timesCalled int
	remoteAddr string
}

func (t *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.timesCalled++
	t.remoteAddr = r.RemoteAddr
}

func TestProxyMiddleware(t *testing.T) {
	tests := []struct {
		name   string
		proxy config.Proxy
		r *http.Request
		expectedAddr string
	}{
		{
			name: "no proxy should forward default remote addr",
			proxy: config.Proxy{},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
			},
			expectedAddr: "127.0.0.1:1234",
		},
		{
			name: "proxy should forward proxy header X-Forwarded-For if set",
			proxy: config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"X-Forwarded-For": []string{"10.0.0.1, 10.3.2.1"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should forward proxy header X-Real-IP if set",
			proxy: config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"X-Real-Ip": []string{"10.0.0.1, 10.3.2.1"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should forward proxy header Forwarded if set",
			proxy: config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"for=10.0.0.1, for=10.0.0.3"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should forward custom proxy header if set",
			proxy: config.Proxy{
				Enable: true,
				Header: "X-My-Proxy-Header",
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"X-My-Proxy-Header": []string{"10.1.0.1"},
					"X-Forwarded-For": []string{"10.0.0.1, 10.3.2.1"},
				},
			},
			expectedAddr: "10.1.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			handler := &testHandler{}

			middleware := NewProxyMiddleware(tt.proxy, handler)

			middleware.ServeHTTP(nil, tt.r)
			
			if handler.remoteAddr != tt.expectedAddr {
				t.Errorf("Expected %s, got %s", tt.expectedAddr, handler.remoteAddr)
			}

			if handler.timesCalled != 1 {
				t.Errorf("Expected handler to be called once, got %d", handler.timesCalled)
			}
		})
	}
}
