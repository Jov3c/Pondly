package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsDeclaredProxiesOnly(t *testing.T) {
	request := func(forwarded string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/public/feed", nil)
		r.RemoteAddr = (&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 40000}).String()
		if forwarded != "" {
			r.Header.Set("X-Forwarded-For", forwarded)
		}
		return r
	}

	if got := clientIPFrom(request("203.0.113.7"), nil); got != "127.0.0.1" {
		t.Fatalf("without declared proxies a forged XFF must be ignored, got %q", got)
	}

	trusted := []string{"127.0.0.0/8"}
	if got := clientIPFrom(request("203.0.113.7"), trusted); got != "203.0.113.7" {
		t.Fatalf("declared proxy must be honoured, got %q", got)
	}
	if got := clientIPFrom(request("198.51.100.9, 203.0.113.7, 127.0.0.1"), trusted); got != "203.0.113.7" {
		t.Fatalf("must stop at the first untrusted hop, got %q", got)
	}
}
