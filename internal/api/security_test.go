package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	ts, _ := testServer(t, nil)
	resp, err := client.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for h, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "sandbox",
	} {
		if got := resp.Header.Get(h); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", h, got, want)
		}
	}
}

func TestCrossSiteWritesRefused(t *testing.T) {
	ts, _ := testServer(t, nil)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/settings", strings.NewReader(`{}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site write without Origin: want 403, got %d", resp.StatusCode)
	}
}

func TestClientIPIgnoresSpoofedHeaders(t *testing.T) {
	s := &Server{TrustProxy: true}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Real-IP", "203.0.113.7") // set by nginx
	r.Header.Set("CF-Connecting-IP", "1.2.3.4")
	if got := s.clientIP(r); got != "203.0.113.7" {
		t.Errorf("X-Real-IP should win, got %s", got)
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9") // first entry forged by the client
	if got := s.clientIP(r); got != "203.0.113.9" {
		t.Errorf("want the proxy-appended hop, got %s", got)
	}
	if got := (&Server{}).clientIP(r); got == "203.0.113.9" || got == "6.6.6.6" {
		t.Errorf("headers trusted without TrustProxy: %s", got)
	}
}

func TestLoginLimitedPerAccount(t *testing.T) {
	ts, _ := testServer(t, nil)
	anon := &http.Client{}
	var last int
	for i := 0; i < 12; i++ {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/login",
			strings.NewReader(`{"email":"victim@example.com","password":"wrong password"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i)) // a new IP every try
		resp, err := anon.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("12th attempt on one account: want 429, got %d", last)
	}
}
