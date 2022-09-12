package middleware

import (
	"net/http"
	"strings"

	"github.com/contentsquare/chproxy/config"
)

const (
	xForwardedForHeader = "X-Forwarded-For"
	xRealIPHeader       = "X-Real-Ip"
	forwardedHeader     = "Forwarded"
)

type ProxyMiddleware struct {
	proxy config.Proxy

	next http.Handler
}

func NewProxyMiddleware(proxy config.Proxy, next http.Handler) *ProxyMiddleware {
	return &ProxyMiddleware{
		proxy: proxy,
		next:  next,
	}
}

func (m *ProxyMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.RemoteAddr = m.getIP(r)
	m.next.ServeHTTP(w, r)
}

func (m *ProxyMiddleware) getIP(r *http.Request) string {
	if m.proxy.Enable {
		if m.proxy.Header != "" {
			return r.Header.Get(m.proxy.Header)
		} else {
			return parseDefaultProxyHeaders(r)
		}
	}

	return r.RemoteAddr
}

func parseDefaultProxyHeaders(r *http.Request) string {
	var addr string

	if fwd := r.Header.Get(xForwardedForHeader); fwd != "" {
		addr = extractFirstMatchFromIPList(fwd)
	} else if fwd := r.Header.Get(xRealIPHeader); fwd != "" {
		addr = extractFirstMatchFromIPList(fwd)
	} else if fwd := r.Header.Get(forwardedHeader); fwd != "" {
		// See: https://tools.ietf.org/html/rfc7239.
		addr = parseForwardedHeader(fwd)
	}

	return addr
}

func extractFirstMatchFromIPList(ipList string) string {
	if ipList == "" {
		return ""
	}
	s := strings.Index(ipList, ", ")
	if s == -1 {
		s = len(ipList)
	}

	return ipList[:s]
}

func parseForwardedHeader(fwd string) string {
	splits := strings.Split(fwd, ";")
	if len(splits) == 0 {
		return ""
	}

	for _, split := range splits {
		trimmed := strings.TrimSpace(split)
		if strings.HasPrefix(trimmed, "for=") {
			forSplits := strings.Split(trimmed, ", ")
			if len(forSplits) == 0 {
				return ""
			}

			return forSplits[0][4:]
		}
	}

	return ""
}
