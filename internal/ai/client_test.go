package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateBaseURL(t *testing.T) {
	ok := []string{"http://localhost:8080", "http://127.0.0.1:9", "https://api.deepseek.com", "http://[::1]:1"}
	for _, u := range ok {
		if err := ValidateBaseURL(u); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v, want nil", u, err)
		}
	}
	bad := map[string]string{
		"http://api.deepseek.com":            "remote http must be rejected",
		"https://user:pass@api.deepseek.com": "userinfo must be rejected",
		"ftp://localhost":                    "non-http scheme rejected",
		"":                                   "empty rejected",
		// SSRF / internal-host guard (all https, non-local literal IPs):
		"https://169.254.169.254":    "link-local metadata must be rejected",
		"https://169.254.169.254/v1": "link-local metadata must be rejected",
		"https://10.0.0.5:8443":      "RFC1918 10/8 must be rejected",
		"https://172.16.0.1":         "RFC1918 172.16/12 must be rejected",
		"https://192.168.1.10":       "RFC1918 192.168/16 must be rejected",
		"https://[fe80::1]":          "IPv6 link-local must be rejected",
		"https://[fd00::1]":          "IPv6 unique-local must be rejected",
		"https://0.0.0.0":            "unspecified address must be rejected",
	}
	for u, why := range bad {
		if err := ValidateBaseURL(u); err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want error (%s)", u, why)
		}
	}
}

func TestChatCompletionsURLNormalization(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com":     "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/":    "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1":  "https://api.deepseek.com/v1/chat/completions",
		"https://api.deepseek.com/v1/": "https://api.deepseek.com/v1/chat/completions",
	}
	for in, want := range cases {
		if got := chatCompletionsURL(in); got != want {
			t.Errorf("chatCompletionsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientHitsV1ChatCompletionsForBothBaseForms(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL, srv.URL + "/v1"} {
		gotPath = ""
		c, err := NewClient(base, "m", "k", time.Second)
		if err != nil {
			t.Fatalf("NewClient(%q): %v", base, err)
		}
		if _, err := c.Complete(context.Background(), "s", "u"); err != nil {
			t.Fatal(err)
		}
		if gotPath != "/v1/chat/completions" {
			t.Errorf("base %q -> path %q, want /v1/chat/completions", base, gotPath)
		}
	}
}

func TestClientRefusesRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/collect", http.StatusFound)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, "m", "SECRETKEY", time.Second)
	_, err := c.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected error: redirects must be refused")
	}
	if strings.Contains(err.Error(), "SECRETKEY") {
		t.Errorf("error leaked secret: %v", err)
	}
}

func TestClientCompleteSuccess(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"metrics\":[],\"summary\":\"ok\"}"}}]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "deepseek-v4-flash", "SECRET", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	content, err := c.Complete(context.Background(), "sys", "user-body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "summary") {
		t.Errorf("content = %q", content)
	}
	if gotAuth != "Bearer SECRET" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if !strings.Contains(gotBody, "user-body") {
		t.Errorf("request body missing user content: %q", gotBody)
	}
}

func TestClientCompleteHTTPErrorNoHeaderLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, "m", "SECRETKEY", time.Second)
	_, err := c.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if strings.Contains(err.Error(), "SECRETKEY") || strings.Contains(strings.ToLower(err.Error()), "authorization") {
		t.Errorf("error leaked secret/header: %v", err)
	}
}
