package provider

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type debugTransport struct {
	base        http.RoundTripper
	logPath     string
	providerTag string
}

func (t debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	// Copy request body so downstream handlers still see it.
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	t.appendBlock("--- DEBUG provider request ---", fmt.Sprintf(
		"provider=%s\n%s %s\nBody:\n%s\n",
		t.providerTag,
		req.Method,
		req.URL.String(),
		string(reqBody),
	))

	resp, err := base.RoundTrip(req)
	if err != nil {
		t.appendBlock("--- DEBUG provider response ---", fmt.Sprintf("provider=%s\nerror: %v\n", t.providerTag, err))
		return nil, err
	}

	if shouldSkipDebugResponseBody(req, resp) {
		t.appendBlock("--- DEBUG provider response ---", fmt.Sprintf(
			"provider=%s\nStatus: %s\nBody: <skipped: streaming>\n",
			t.providerTag,
			resp.Status,
		))
		return resp, nil
	}

	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))

	t.appendBlock("--- DEBUG provider response ---", fmt.Sprintf(
		"provider=%s\nStatus: %s\nBody:\n%s\n",
		t.providerTag,
		resp.Status,
		string(body),
	))

	return resp, nil
}

func shouldSkipDebugResponseBody(req *http.Request, resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return true
	}
	if strings.EqualFold(req.URL.Query().Get("alt"), "sse") {
		return true
	}
	// Heuristic: some streaming endpoints don't set Content-Type early.
	if strings.Contains(strings.ToLower(req.URL.Path), "stream") {
		return true
	}
	return false
}

func (t debugTransport) appendBlock(title, payload string) {
	if t.logPath == "" {
		return
	}
	f, err := os.OpenFile(t.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	// Keep it append-only and easy to grep.
	_, _ = fmt.Fprintln(f)
	_, _ = fmt.Fprintln(f, title)
	_, _ = fmt.Fprint(f, payload)
}

func maybeEnableHTTPDebug(client *http.Client, cfg map[string]string, providerTag string) {
	if client == nil {
		return
	}
	if !strings.EqualFold(cfg["_log_level"], "DEBUG") {
		return
	}
	logPath := cfg["_log_path"]
	if strings.TrimSpace(logPath) == "" {
		return
	}
	client.Transport = debugTransport{
		base:        client.Transport,
		logPath:     logPath,
		providerTag: providerTag,
	}
}
