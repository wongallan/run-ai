package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	copilotClientID       = "Ov23lihVA6IPSeMxp4BB"
	copilotScope          = "read:user"
	defaultCopilotBaseURL = "https://api.githubcopilot.com"
	oauthPollingMarginMs  = 500
)

var openBrowser = openBrowserDefault

// CopilotAuth holds the result of a successful GitHub Copilot authentication.
type CopilotAuth struct {
	Token         string
	EnterpriseURL string // empty for github.com
}

// NormalizeDomain strips protocol, port, and trailing slashes from a URL or
// domain string.  For example "https://company.ghe.com/" becomes "company.ghe.com".
func NormalizeDomain(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if !strings.Contains(input, "://") {
		input = "https://" + input
	}
	u, err := url.Parse(input)
	if err != nil {
		return strings.TrimRight(strings.TrimSpace(input), "/")
	}
	host := u.Hostname()
	if host == "" {
		return strings.TrimRight(strings.TrimSpace(input), "/")
	}
	return host
}

// CopilotBaseURL returns the Copilot API base URL for the given enterprise
// domain.  An empty string (or "github.com") returns the default public
// Copilot endpoint; anything else builds "https://copilot-api.{domain}".
func CopilotBaseURL(enterpriseURL string) string {
	if enterpriseURL == "" {
		return defaultCopilotBaseURL
	}
	domain := NormalizeDomain(enterpriseURL)
	if domain == "" || domain == "github.com" {
		return defaultCopilotBaseURL
	}
	return "https://copilot-api." + domain
}

// oauthURLs returns the device-code and access-token OAuth endpoints for a
// given domain.
func oauthURLs(domain string) (deviceCodeURL, accessTokenURL string) {
	if domain == "" || domain == "github.com" {
		return "https://github.com/login/device/code",
			"https://github.com/login/oauth/access_token"
	}
	return fmt.Sprintf("https://%s/login/device/code", domain),
		fmt.Sprintf("https://%s/login/oauth/access_token", domain)
}

// DeviceAuth performs an OAuth device-code flow for GitHub Copilot.
// It writes instructions to w and blocks until the user completes
// authentication or the context is cancelled.
func DeviceAuth(ctx context.Context, domain string, w io.Writer) (*CopilotAuth, error) {
	deviceURL, tokenURL := oauthURLs(domain)

	// Step 1: request a device code.
	devicePayload, _ := json.Marshal(map[string]string{
		"client_id": copilotClientID,
		"scope":     copilotScope,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", deviceURL, bytes.NewReader(devicePayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed (HTTP %d): %s", resp.StatusCode, body)
	}

	var deviceData struct {
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		UserCode                string `json:"user_code"`
		DeviceCode              string `json:"device_code"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &deviceData); err != nil {
		return nil, fmt.Errorf("parsing device response: %w", err)
	}

	// Step 2: instruct the user.
	verificationURL := deviceData.VerificationURIComplete
	if verificationURL == "" {
		verificationURL = deviceData.VerificationURI
	}
	if verificationURL != "" {
		if err := openBrowser(verificationURL); err == nil {
			fmt.Fprintln(w, "Opening browser for authentication...")
		}
	}
	fmt.Fprintf(w, "Open %s and enter code: %s\n", deviceData.VerificationURI, deviceData.UserCode)
	fmt.Fprintln(w, "Waiting for authentication...")

	// Step 3: poll for the access token.
	interval := time.Duration(deviceData.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval + time.Duration(oauthPollingMarginMs)*time.Millisecond):
		}

		tokenPayload, _ := json.Marshal(map[string]string{
			"client_id":   copilotClientID,
			"device_code": deviceData.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})

		pollReq, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewReader(tokenPayload))
		if err != nil {
			return nil, err
		}
		pollReq.Header.Set("Accept", "application/json")
		pollReq.Header.Set("Content-Type", "application/json")

		pollResp, err := http.DefaultClient.Do(pollReq)
		if err != nil {
			return nil, fmt.Errorf("token poll: %w", err)
		}

		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("token request failed (HTTP %d): %s", pollResp.StatusCode, pollBody)
		}

		var tokenData struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Interval    int    `json:"interval"`
		}
		if err := json.Unmarshal(pollBody, &tokenData); err != nil {
			return nil, fmt.Errorf("parsing token response: %w", err)
		}

		if tokenData.AccessToken != "" {
			return &CopilotAuth{
				Token:         tokenData.AccessToken,
				EnterpriseURL: domain,
			}, nil
		}

		switch tokenData.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			// RFC 8628 §3.5: add 5 seconds to current interval.
			newInterval := deviceData.Interval + 5
			if tokenData.Interval > 0 {
				newInterval = tokenData.Interval
			}
			interval = time.Duration(newInterval) * time.Second
			continue
		case "":
			continue
		default:
			return nil, fmt.Errorf("authentication failed: %s", tokenData.Error)
		}
	}
}

func openBrowserDefault(target string) error {
	if target == "" {
		return fmt.Errorf("missing URL")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	return cmd.Start()
}

// LoadCopilotToken reads a stored Copilot token from .rai/copilot-token.
func LoadCopilotToken(baseDir string) string {
	path := filepath.Join(baseDir, ".rai", "copilot-token")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SaveCopilotToken persists a Copilot token to .rai/copilot-token (mode 0600).
func SaveCopilotToken(baseDir, token string) error {
	dir := filepath.Join(baseDir, ".rai")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "copilot-token"), []byte(token+"\n"), 0o600)
}
