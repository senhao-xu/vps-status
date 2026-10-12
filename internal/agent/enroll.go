package agent

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

const maxEnrollmentResponseBytes = 64 << 10

// Enroll exchanges a one-time token for a node credential. The token is sent
// only in the Authorization header.
func Enroll(ctx context.Context, client *http.Client, controlURL, token string) (Config, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base, err := url.Parse(strings.TrimRight(controlURL, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || token == "" || !secureOrLoopbackHTTP(base) {
		return Config{}, errors.New("Control URL is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/agent/v1/enroll"
	base.RawQuery, base.Fragment = "", ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), nil)
	if err != nil {
		return Config{}, fmt.Errorf("create enrollment request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	requestClient := *client
	// Enrollment has one fixed endpoint. Refusing redirects prevents the
	// Authorization credential from ever being forwarded to another URL or
	// downgraded from HTTPS to plaintext HTTP.
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := requestClient.Do(request)
	if err != nil {
		return Config{}, fmt.Errorf("enroll Agent: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxEnrollmentResponseBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read enrollment response: %w", err)
	}
	if len(body) > maxEnrollmentResponseBytes {
		return Config{}, errors.New("enrollment response exceeds the size limit")
	}
	if response.StatusCode != http.StatusCreated {
		return Config{}, fmt.Errorf("enrollment rejected with status %d", response.StatusCode)
	}
	var result struct {
		NodeID     string `json:"node_id"`
		Credential string `json:"credential"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || result.NodeID == "" || result.Credential == "" {
		return Config{}, errors.New("enrollment response is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("enrollment response is invalid")
	}
	return Config{ControlURL: controlURL, NodeID: result.NodeID, Credential: result.Credential}, nil
}

func secureOrLoopbackHTTP(value *url.URL) bool {
	if value.Scheme == "https" {
		return true
	}
	if value.Scheme != "http" {
		return false
	}
	host := value.Hostname()
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
}
