package verify

import (
	"context"
	"encoding/json"
	"fmt"
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
// Unlike command checks, this kind reads its API key, API origin, and CI base
// branch from the host environment without pass_env: the kind itself defines
// which variables it consumes, so consumers only add a secret.
func semanticLint(ctx context.Context, req Request, dir string, env []string) Result {
	options := req.Check.semanticOptions()
	timeout := 5 * time.Minute
	if options.Timeout != "" {
		parsed, err := time.ParseDuration(options.Timeout)
		if err != nil || parsed <= 0 {
			return Result{Status: StatusError, Error: "invalid timeout"}
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	apiKey := hostValue(env, semanticAPIKeyEnv)
	if apiKey == "" {
		return Result{Status: StatusError, Error: fmt.Sprintf("%s is not set; export it locally or supply it from a CI secret", semanticAPIKeyEnv)}
	}
	git, err := executable(dir, env, "git")
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}

	model := options.Model
	if model == "" {
		model = semantic.DefaultModel
	}
	base := options.Base
	if base == "" {
		base = hostValue(env, semanticBaseRefEnv) // GitHub sets this for pull requests.
	}
	if base == "" {
		base = semanticDefaultRef
	}
	baseURL := hostValue(env, semanticBaseURLEnv)
	if baseURL == "" {
		baseURL = semantic.DefaultBaseURL
	}
	report, runErr := semantic.Run(ctx, semantic.Options{
		Source:  req.Source,
		Include: semanticScope(req.Target),
		Git:     git,
		Env:     env,
		Base:    base,
		Client:  semantic.Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, Model: model},
	})
	details, _ := json.Marshal(report)
	result := Result{Status: StatusPassed, Stdout: semantic.Summary(report), Details: details}

	switch {
	case ctx.Err() != nil && runErr != nil:
		if err := req.deadline(ctx); err != nil {
			return result.withOutcome(StatusError, "semantic-lint timed out")
		}
		return result.withOutcome(StatusCancelled, ctx.Err().Error())
	case runErr != nil:
		return result.withOutcome(StatusError, runErr.Error())
	case len(report.Missing) > 0:
		return result.withOutcome(StatusIncomplete, fmt.Sprintf("%d questions were not answered", len(report.Missing)))
	}
	return result
}

// deadline distinguishes the check's own timeout from an outer cancellation.
func (req Request) deadline(ctx context.Context) error {
	if ctx.Err() == context.DeadlineExceeded {
		return ctx.Err()
	}
	return nil
}

// hostValue prefers a value declared through env or pass_env, then falls back
// to the runner's own environment.
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
