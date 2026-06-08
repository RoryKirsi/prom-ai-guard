package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errNoRedirect refuses redirects so an Authorization-bearing request is never
// re-sent to a redirect target.
var errNoRedirect = errors.New("redirects are not allowed")

// Completer performs a single chat-completion round trip and returns the
// assistant message content. It does not retry — the analyzer owns the retry
// loop so that invalid-JSON responses can also be retried.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Client is an OpenAI-compatible Chat Completions LLM client (DeepSeek is the
// default provider, but any OpenAI-compatible endpoint works). The API key is
// held only here, never serialized, logged, or echoed in errors.
type Client struct {
	endpoint   string // fully resolved chat-completions URL
	model      string
	apiKey     string
	httpClient *http.Client
}

// NewClient validates the base URL and constructs a client. It does not perform
// any network call.
func NewClient(baseURL, model, apiKey string, timeout time.Duration) (*Client, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		endpoint: chatCompletionsURL(baseURL),
		model:    model,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errNoRedirect
			},
		},
	}, nil
}

// chatCompletionsURL resolves the endpoint, accepting a base URL with or without
// a trailing "/v1" so both forms call .../v1/chat/completions exactly once.
func chatCompletionsURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	return base + "/v1/chat/completions"
}

// ValidateBaseURL enforces: a parseable http/https URL, no userinfo, and https
// for any non-local host. localhost/127.0.0.1/::1 may use http (for tests).
func ValidateBaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("base URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid base URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base URL scheme must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("base URL must not contain userinfo")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("base URL has no host")
	}
	if isLocalHost(host) {
		return nil // explicitly allowed for local/test servers
	}
	if u.Scheme != "https" {
		return fmt.Errorf("base URL must use https for non-local hosts")
	}
	// For non-local hosts given as a literal IP, block ranges that would enable
	// SSRF / credential exfiltration to internal or metadata endpoints.
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("base URL host is not allowed")
	}
	return nil
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

// isBlockedIP reports whether an IP falls in a range we refuse to send the
// Authorization-bearing request to: loopback, link-local (incl. 169.254.0.0/16
// and fe80::/10), RFC1918/RFC4193 private, and the unspecified address.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified()
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Complete sends one request. Errors are deliberately header-free: they never
// include the Authorization header, the API key, or the request body.
func (c *Client) Complete(ctx context.Context, system, user string) (string, error) {
	reqBody, err := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0,
	})
	if err != nil {
		return "", fmt.Errorf("encoding request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("building request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// net errors do not contain the key/header; still wrap with a fixed prefix.
		return "", fmt.Errorf("llm request failed: transport error")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm request failed: status %d", resp.StatusCode)
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil || len(cr.Choices) == 0 {
		return "", fmt.Errorf("llm response missing choices")
	}
	return cr.Choices[0].Message.Content, nil
}
