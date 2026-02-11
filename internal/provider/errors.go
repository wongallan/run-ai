package provider

import (
	"fmt"
	"strings"
)

// ProviderError is a structured error with actionable guidance for users.
// All provider HTTP errors are normalized into this type so the CLI can
// display consistent, helpful messages regardless of which backend is used.
type ProviderError struct {
	StatusCode int
	Provider   string
	Message    string
	Guidance   string
}

func (e *ProviderError) Error() string {
	parts := []string{fmt.Sprintf("%s: %s", e.Provider, e.Message)}
	if e.Guidance != "" {
		parts = append(parts, e.Guidance)
	}
	return strings.Join(parts, " — ")
}

// NormalizeHTTPError converts a raw HTTP status code and response body into
// a ProviderError with actionable guidance.
func NormalizeHTTPError(providerName string, statusCode int, body string) *ProviderError {
	pe := &ProviderError{
		StatusCode: statusCode,
		Provider:   providerName,
	}

	switch {
	case statusCode == 401:
		pe.Message = "authentication failed"
		pe.Guidance = "verify your API key with 'rai config api-key <key>' or set RAI_API_KEY"
	case statusCode == 403:
		pe.Message = "access denied"
		pe.Guidance = "check your API key permissions and account status"
	case statusCode == 404:
		pe.Message = "endpoint or model not found"
		pe.Guidance = "verify your endpoint with 'rai config endpoint <url>' and model with 'rai config model <name>'"
	case statusCode == 429:
		pe.Message = "rate limited"
		pe.Guidance = "wait a moment and try again, or check your usage quota"
	case statusCode >= 500:
		pe.Message = fmt.Sprintf("server error (HTTP %d)", statusCode)
		pe.Guidance = "the provider may be experiencing issues; try again shortly"
	default:
		pe.Message = fmt.Sprintf("unexpected error (HTTP %d)", statusCode)
		if body != "" {
			// Truncate long error bodies.
			if len(body) > 200 {
				body = body[:200] + "..."
			}
			pe.Message += ": " + body
		}
	}

	return pe
}
