// Package desktop provides the optional Windows notification-area controls.
package desktop

import (
	"net"
	"net/url"
)

// LocalURL converts wildcard listeners into an address a local browser can open.
func LocalURL(address string, secure bool) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}).String()
}
