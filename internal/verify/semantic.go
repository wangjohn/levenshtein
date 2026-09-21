package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wangjohn/levenshtein/internal/semantic"
)

const (
	semanticAPIKeyEnv  = "TYPESAFE_API_KEY"
	semanticBaseURLEnv = "TYPESAFE_BASE_URL"
	semanticBaseRefEnv = "GITHUB_BASE_REF"
	semanticDefaultRef = "main"
)

// semanticLint asks a pinned Jev model the shared catalog's questions about the
// change and reports advisory findings. It always executes; nothing is cached.
//
// Unlike command checks, this kind names the variables it consumes, so a
// consumer adds a secret without a pass_env entry. The API key and the API
// origin are read from the host process alone: committed configuration can
// neither supply the key nor redirect it to another origin. Only the CI base
// branch may also come from the check's environment.
func semanticLint(ctx context.Context, req Request, dir string, env []string) Result {
	timeout := 5 * time.Minute
	if req.Check.Timeout != "" {
		parsed, err := time.ParseDuration(req.Check.Timeout)
		if err != nil || parsed <= 0 {
			return Result{Status: StatusError, Error: "invalid timeout"}
		}
		timeout = parsed
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	apiKey := os.Getenv(semanticAPIKeyEnv)
	if apiKey == "" {
		return Result{Status: StatusError, Error: fmt.Sprintf("%s is not set; export it locally or supply it from a CI secret", semanticAPIKeyEnv)}
	}
	git, err := executable(dir, env, "git")
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}

	model := req.Check.Model
	if model == "" {
		model = semantic.DefaultModel
	}
	base := req.Check.Base
	if base == "" {
		base = hostValue(env, semanticBaseRefEnv) // GitHub sets this for pull requests.
	}
	if base == "" {
		base = semanticDefaultRef
	}
	baseURL := os.Getenv(semanticBaseURLEnv)
	if baseURL == "" {
		baseURL = semantic.DefaultBaseURL
	}
	origin, err := semanticOrigin(baseURL)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}
	report, runErr := semantic.Run(ctx, semantic.Options{
		Source:  req.Source,
		Include: semanticScope(req.Target),
		Git:     git,
		Env:     env,
		Base:    base,
		Client:  semantic.Client{BaseURL: origin, APIKey: apiKey, Model: model},
	})
	details, _ := json.Marshal(report)
	result := Result{Status: StatusPassed, Stdout: semantic.Summary(report), Details: details}

	switch {
	case ctx.Err() != nil && runErr != nil:
		// The parent carries the run's own cancellation or deadline; only the
		// timeout above belongs to this check.
		if errors.Is(parent.Err(), context.Canceled) || errors.Is(parent.Err(), context.DeadlineExceeded) {
			return result.withOutcome(StatusCancelled, parent.Err().Error())
		}
		return result.withOutcome(StatusError, "semantic-lint timed out")
	case runErr != nil:
		return result.withOutcome(StatusError, runErr.Error())
	case len(report.Missing) > 0:
		return result.withOutcome(StatusIncomplete, fmt.Sprintf("%d questions were not answered", len(report.Missing)))
	}
	return result
}

// semanticLoopbackHosts may serve the API over plain HTTP, which keeps local
// httptest servers and recording proxies usable.
var semanticLoopbackHosts = map[string]bool{"127.0.0.1": true, "::1": true, "localhost": true}

// semanticOrigin rejects an origin that would put the bearer token on the wire
// in the clear.
func semanticOrigin(raw string) (string, error) {
	trimmed := strings.TrimRight(raw, "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("%s %q is not a valid URL", semanticBaseURLEnv, raw)
	}
	if parsed.Scheme != "https" && !semanticLoopbackHosts[parsed.Hostname()] {
		return "", fmt.Errorf("%s %q must use https so the API key is not sent in the clear; plain http is accepted only for loopback hosts", semanticBaseURLEnv, raw)
	}
	return trimmed, nil
}

// hostValue prefers a value declared through env or pass_env, then falls back
// to the runner's own environment. Credentials never read through it.
func hostValue(env []string, name string) string {
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, name+"="); ok {
			return value
		}
	}
	return os.Getenv(name)
}

// semanticScope limits judged files to the target's directory and declared
// inputs, and skips fixtures and private files.
func semanticScope(target Target) func(string) bool {
	return func(path string) bool {
		if privateSourcePath(path) || !relative(path) {
			return false
		}
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			if part == "testdata" {
				return false
			}
		}
		if target.Dir != "." && !within(path, target.Dir) {
			return false
		}
		for _, input := range target.Inputs {
			if input == "." || within(path, input) {
				return true
			}
		}
		return false
	}
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
