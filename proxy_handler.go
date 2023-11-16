package main

import (
	"net"
	"net/http"
	"strings"

	"github.com/contentsquare/chproxy/config"
)

const (
	xForwardedForHeader = "X-Forwarded-For"
	xRealIPHeader       = "X-Real-Ip"
	forwardedHeader     = "Forwarded"
)

type ProxyHandler struct {
	proxy *config.Proxy
}

func NewProxyHandler(proxy *config.Proxy) *ProxyHandler {
	return &ProxyHandler{
		proxy: proxy,
	}
}

func (m *ProxyHandler) GetRemoteAddr(r *http.Request) string {
	if m.proxy.Enable {
		var addr string
		if m.proxy.Header != "" {
			addr = r.Header.Get(m.proxy.Header)
		} else {
			addr = parseDefaultProxyHeaders(r)
		}

		if isValidAddr(addr) {
			return addr
		}
	}

	return r.RemoteAddr
}

// isValidAddr checks if the Addr is a valid IP or IP:port.
func isValidAddr(addr string) bool {
	if addr == "" {
		return false
	}

	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return net.ParseIP(addr) != nil
	}

	return net.ParseIP(ip) != nil
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
	s := strings.Index(ipList, ",")
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
		if strings.HasPrefix(strings.ToLower(trimmed), "for=") {
			forSplits := strings.Split(trimmed, ",")
			if len(forSplits) == 0 {
				return ""
			}

			addr := forSplits[0][4:]
			trimmedAddr := strings.
				NewReplacer("\"", "", "[", "", "]", "").
				Replace(addr) // If IpV6, remove brackets and quotes.

			return trimmedAddr
		}
	}

	return ""
}
