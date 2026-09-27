// Package semantic asks TypeSafe's Jev model bounded questions about a change
// and composes the typed answers into advisory findings. Code selects what to
// judge and owns thresholds; the model only answers.
package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultModel is the pinned release thresholds were written against.
const DefaultModel = "jev-1.13.0"

// DefaultBaseURL is TypeSafe's public API origin.
const DefaultBaseURL = "https://api.typesafe.ai"

// Primitive is a Jev question type.
type Primitive string

const (
	PrimitiveNoul  Primitive = "noul"
	PrimitiveScore Primitive = "score"
)

type wireQuestion struct {
	Type         Primitive `json:"type"`
	Instructions any       `json:"instructions"`
	Criteria     any       `json:"criteria,omitempty"`
}

type wireRequest struct {
	State     any                     `json:"state"`
	Model     string                  `json:"model"`
	Questions map[string]wireQuestion `json:"questions"`
}

type wireUsage struct {
	InputTokens int `json:"input_tokens"`
}

// Answer is one typed judgment. Noul carries only a probability; Score also
// carries a confidence derived from its level distribution.
type Answer struct {
	Type          Primitive          `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

type wireResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   wireUsage         `json:"usage"`
}

// Client calls the System One endpoint with a pinned model.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
	// AttemptTimeout bounds one request, so a stalled connection is retried
	// instead of holding the check until its own timeout. Zero means 90 seconds.
	AttemptTimeout time.Duration
	// sleep waits between attempts; tests replace it to skip the wait.
	sleep func(context.Context, time.Duration) error
}

// APIError reports a definitive rejection that retrying cannot fix.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("TypeSafe API returned HTTP %d: %s", e.Status, e.Body)
}

const (
	maxAttempts           = 3
	defaultAttemptTimeout = 90 * time.Second
)

// Ask evaluates every question against one state in a single parallel pass.
// Rate limiting, overload, server and gateway failures, and a stalled attempt
// are retried; other failures are returned.
func (c Client) Ask(ctx context.Context, state any, questions map[string]wireQuestion) (wireResponse, error) {
	body, err := json.Marshal(wireRequest{State: state, Model: c.Model, Questions: questions})
	if err != nil {
		return wireResponse{}, err
	}
	sleep := c.sleep
	if sleep == nil {
		sleep = sleepContext
	}

	var last error
	var delay time.Duration
	for attempt := range maxAttempts {
		// Only wait before another attempt; after the last there is none.
		if attempt > 0 {
			if err := sleep(ctx, delay); err != nil {
				return wireResponse{}, err
			}
		}
		response, retryAfter, err := c.attempt(ctx, body)
		if err == nil {
			return response, nil
		}
		last = err
		if retryAfter < 0 {
			return wireResponse{}, err
		}
		delay = retryAfter
	}
	return wireResponse{}, fmt.Errorf("gave up after %d attempts: %w", maxAttempts, last)
}

// attempt posts once under the per-attempt timeout. Running out of that time,
// as opposed to the caller's, is retryable.
func (c Client) attempt(ctx context.Context, body []byte) (wireResponse, time.Duration, error) {
	timeout := c.AttemptTimeout
	if timeout <= 0 {
		timeout = defaultAttemptTimeout
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, retryAfter, err := c.post(attemptCtx, body)
	if err != nil && ctx.Err() == nil && attemptCtx.Err() != nil {
		return wireResponse{}, time.Second, fmt.Errorf("TypeSafe API did not answer within %s", timeout)
	}
	return response, retryAfter, err
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// noRedirectClient never follows a redirect, so the bearer token is only ever
// sent to the configured origin. A 3xx answer surfaces as an API error.
var noRedirectClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// post returns a negative retry delay when the failure is not retryable.
func (c Client) post(ctx context.Context, body []byte) (wireResponse, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return wireResponse{}, -1, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		client = noRedirectClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return wireResponse{}, -1, ctx.Err()
		}
		return wireResponse{}, time.Second, err
	}
	defer func() { _ = resp.Body.Close() }() // Response bytes were fully read or the failure is already reported.

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return wireResponse{}, time.Second, err
	}
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return wireResponse{}, -1, fmt.Errorf("TypeSafe API redirected to %q; redirects are not followed so the key only reaches the configured origin", resp.Header.Get("Location"))
	case http.StatusOK:
		var response wireResponse
		if err := json.Unmarshal(data, &response); err != nil {
			return wireResponse{}, -1, fmt.Errorf("TypeSafe API returned malformed JSON: %w", err)
		}
		return response, 0, nil
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return wireResponse{}, retryDelay(resp.Header.Get("Retry-After")), &APIError{Status: resp.StatusCode, Body: summary(data)}
	default:
		return wireResponse{}, -1, &APIError{Status: resp.StatusCode, Body: summary(data)}
	}
}

func retryDelay(header string) time.Duration {
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds <= 0 {
		return 2 * time.Second
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

// summary shortens an error body on a rune boundary, so a reported failure
// never ends in a broken character.
func summary(data []byte) string {
	const limit = 400
	text := bytes.TrimSpace(data)
	if len(text) <= limit {
		return string(text)
	}
	return string(text[:runeBoundary(string(text), limit)]) + "..."
}
