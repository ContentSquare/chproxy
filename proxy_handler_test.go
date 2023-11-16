package main

import (
	"net/http"
	"testing"

	"github.com/contentsquare/chproxy/config"
)

func TestProxyHandler(t *testing.T) {
	tests := []struct {
		name         string
		proxy        *config.Proxy
		r            *http.Request
		expectedAddr string
	}{
		{
			name:  "no proxy should forward default remote addr",
			proxy: &config.Proxy{},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
			},
			expectedAddr: "127.0.0.1:1234",
		},
		{
			name: "proxy should forward proxy header X-Forwarded-For if set",
			proxy: &config.Proxy{
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
			name: "proxy should ignore invalid IP values in the Proxy header",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"X-Forwarded-For": []string{"nonsense"},
				},
			},
			expectedAddr: "127.0.0.1:1234",
		},
		{
			name: "proxy should forward proxy header X-Real-IP if set",
			proxy: &config.Proxy{
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
			proxy: &config.Proxy{
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
			name: "proxy should forward proxy header Forwarded if set and treat a lack of spaces as an equivalent (issue #326).",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"for=10.0.0.1,for=10.0.0.3"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should properly parse Forwarded header",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"for=10.0.0.1;proto=http;by=203.0.113.43"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should parse Forwarded header in a case insensitive manner",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"For=10.0.0.1"},
				},
			},
			expectedAddr: "10.0.0.1",
		},
		{
			name: "proxy should parse IPv6 in Forwarded header",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"for=\"[2001:db8:cafe::17]\""},
				},
			},
			expectedAddr: "2001:db8:cafe::17",
		},
		{
			name: "proxy should parse IPv6 + port in Forwarded header",
			proxy: &config.Proxy{
				Enable: true,
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"Forwarded": []string{"for=\"[2001:db8:cafe::17]:4711\""},
				},
			},
			expectedAddr: "2001:db8:cafe::17:4711",
		},
		{
			name: "proxy should forward custom proxy header if set",
			proxy: &config.Proxy{
				Enable: true,
				Header: "X-My-Proxy-Header",
			},
			r: &http.Request{
				RemoteAddr: "127.0.0.1:1234",
				Header: http.Header{
					"X-My-Proxy-Header": []string{"10.1.0.1"},
					"X-Forwarded-For":   []string{"10.0.0.1, 10.3.2.1"},
				},
			},
			expectedAddr: "10.1.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewProxyHandler(tt.proxy)

			remoteAddr := handler.GetRemoteAddr(tt.r)

			if remoteAddr != tt.expectedAddr {
				t.Errorf("Expected %s, got %s", tt.expectedAddr, remoteAddr)
			}
		})
	}
}
