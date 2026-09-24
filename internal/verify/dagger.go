package verify

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dagger.io/dagger"
	"dagger.io/dagger/engineconn"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Dagger shares one SDK session and a stable module across a verification run.
// Consumer sources and fresh-run inputs are function arguments, so they do not
// change the module's download and compiler cache namespace.
type Dagger struct {
	mu     sync.Mutex
	client *dagger.Client
	shared string
}

func (d *Dagger) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.client != nil {
		err := d.client.Close()
		d.client = nil
		d.shared = ""
		return err
	}
	return nil
}

// These are the Dagger functions supported by both planning and execution.
var daggerFunctions = map[CheckKind]string{
	CheckGoLint:           "goLint",
	CheckSelfTest:         "selfTest",
	CheckGoVet:            "sharedCheck",
	CheckGoMod:            "sharedCheck",
	CheckGoTest:           "sharedCheck",
	CheckGoHTTP:           "sharedCheck",
	CheckGoSQL:            "sharedCheck",
	CheckGoVuln:           "sharedCheck",
	CheckWorkflowLint:     "sharedCheck",
	CheckWorkflowSecurity: "sharedCheck",
	CheckGoMutation:       "goMutation",
	CheckGoImports:        "goImports",
	CheckGoGenerate:       "goGenerate",
	CheckGoApidiff:        "goApidiff",
}

func (d *Dagger) Execute(ctx context.Context, req Request) Result {
	if req.Check.Kind == CheckGoMutation {
		return d.executeMutation(ctx, req)
	}
	if req.Check.Kind == CheckGoTest {
		return d.executeTests(ctx, req)
	}
	if req.Check.Kind == CheckGoApidiff {
		return d.executeApidiff(ctx, req)
	}
	result := daggerResult(d.execute(ctx, req, nil))

	if err := ctx.Err(); err != nil {
		return Result{Status: StatusCancelled, Error: err.Error(), Stdout: result.Stdout, Stderr: result.Stderr, Details: result.Details}
	}
	return result
}

// executeMutation selects the files on the host, skips Dagger when there is
// nothing to mutate, and bounds the whole run by the check's timeout.
func (d *Dagger) executeMutation(parent context.Context, req Request) Result {
	options := req.Check.mutationOptions()
	timeout := defaultMutationTimeout
	if options.Timeout != "" {
		parsed, err := time.ParseDuration(options.Timeout)
		if err != nil || parsed <= 0 {
			return Result{Status: StatusError, Error: fmt.Sprintf("invalid go-mutation timeout %q", options.Timeout)}
		}
		timeout = parsed
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	if err := acceptedReachable(req, options.Accepted); err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}
	selection, err := mutationFiles(ctx, req)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}
	if len(selection.Files) == 0 {
		return Result{Status: StatusPassed, Stdout: selection.Note + ": no Go files to mutate"}
	}
	// The session outlives this check: every check in the run shares it, and
	// dagger.Connect ties its lifetime to the context it is given. Connecting
	// with the timeout context would close it for the others once this check
	// returned, so connect with the run's context and bound only the query.
	if err := d.connect(parent, req.Shared); err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}

	lines := ""
	if selection.Lines != nil {
		encoded, err := json.Marshal(selection.Lines)
		if err != nil {
			return Result{Status: StatusError, Error: err.Error()}
		}
		lines = string(encoded)
	}
	var summary string
	result := daggerResult(d.execute(ctx, req, &mutationArgs{files: selection.Files, lines: lines, accepted: options.Accepted, tags: options.Tags, summary: &summary}))
	if raw := mutationSummaryOf(result, summary); raw != "" {
		result.Stdout, result.Details = mutationStdout(raw, selection.Note, result.Details)
	}

	if err := parent.Err(); err != nil {
		return Result{Status: StatusCancelled, Error: err.Error(), Stdout: result.Stdout, Stderr: result.Stderr, Details: result.Details}
	}
	if ctx.Err() != nil {
		return Result{Status: StatusError, Error: fmt.Sprintf("go-mutation exceeded its %s timeout", timeout), Stdout: result.Stdout, Stderr: result.Stderr, Details: result.Details}
	}
	return result
}

// executeTests bounds a go-test query by goCheckTimeout, as the native executor
// bounds its go test process. go test's own -timeout stops a hung test first
// and reports it as a finding; this bound catches whatever outlives that, such
// as a build that never finishes, and makes it an error.
func (d *Dagger) executeTests(parent context.Context, req Request) Result {
	// Connect with the run's context, as executeMutation does, so the shared
	// session outlives this check's bound.
	if err := d.connect(parent, req.Shared); err != nil {
		if parent.Err() != nil {
			return Result{Status: StatusCancelled, Error: parent.Err().Error()}
		}
		return Result{Status: StatusError, Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(parent, goCheckTimeout)
	defer cancel()

	result := daggerResult(d.execute(ctx, req, nil))
	if err := parent.Err(); err != nil {
		return Result{Status: StatusCancelled, Error: err.Error(), Stdout: result.Stdout, Stderr: result.Stderr, Details: result.Details}
	}
	if ctx.Err() != nil {
		return Result{Status: StatusError, Error: fmt.Sprintf("go-test exceeded its %s timeout", goCheckTimeout), Stdout: result.Stdout, Stderr: result.Stderr}
	}
	return result
}

// mutationArgs are the go-mutation function's arguments beyond source and
// module, and where its returned summary lands.
type mutationArgs struct {
	files    []string
	lines    string
	accepted string
	tags     string
	summary  *string
}

func (d *Dagger) execute(ctx context.Context, req Request, mutation *mutationArgs) error {
	function, ok := daggerFunctions[req.Check.Kind]
	if !ok {
		return fmt.Errorf("unsupported Dagger check %q", req.Check.Kind)
	}

	if err := d.connect(ctx, req.Shared); err != nil {
		return err
	}

	// Checks run concurrently, so several goroutines issue queries on this one
	// session after connect returns. The Dagger client is safe for concurrent
	// use; only connect and Close hold mu.

	nonce := executionNonce(req)

	query := d.client.QueryBuilder().Select("levenshtein").Select(function).Arg("nonce", nonce)
	if req.Check.Kind != CheckSelfTest {
		source, err := daggerSource(d.client, req.Source, req.Target.Inputs, req.Target.Exclude)
		if err != nil {
			return err
		}
		query = query.Arg("source", source).Arg("module", req.Target.Dir)
	}
	if function == "sharedCheck" {
		query = query.Arg("check", string(req.Check.Kind))
	}
	// The added patterns are a function argument, so Dagger's own call cache
	// keys on them just as the CLI's fingerprint does.
	if checks := req.Check.lintChecks(); len(checks) > 0 {
		query = query.Arg("checks", checks)
	}
	// A go-imports check's rules are an argument too, so they key Dagger's
	// call cache the way they key the CLI's fingerprint.
	if req.Check.Kind == CheckGoImports {
		rules, err := importRules(req.Check)
		if err != nil {
			return err
		}
		query = query.Arg("rules", rules)
	}
	if mutation != nil {
		query = query.Arg("files", mutation.files).Arg("lines", mutation.lines).Arg("accepted", mutation.accepted).Arg("tags", mutation.tags).Bind(mutation.summary)
	}
	return query.Execute(ctx)
}

func (d *Dagger) connect(ctx context.Context, shared string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	version, err := os.ReadFile(filepath.Join(shared, ".dagger-version"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(version)) != engineconn.CLIVersion {
		return fmt.Errorf("Dagger SDK version does not match .dagger-version")
	}

	if d.client != nil {
		if d.shared != shared {
			return fmt.Errorf("a Dagger session cannot change its shared module")
		}
		return nil
	}

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr))
	if err != nil {
		return err
	}
	if err := client.ModuleSource(shared).AsModule().Serve(ctx); err != nil {
		_ = client.Close()
		return err
	}
	d.client, d.shared = client, shared
	return nil
}

func daggerResult(err error) Result {
	if err == nil {
		return Result{Status: StatusPassed}
	}

	var failure *gqlerror.Error
	if !errors.As(err, &failure) {
		return Result{Status: StatusError, Error: err.Error()}
	}
	stdout, _ := failure.Extensions["stdout"].(string)
	stderr, _ := failure.Extensions["stderr"].(string)
	status := StatusError
	var details json.RawMessage
	if findings, ok := failure.Extensions["levenshteinFindings"]; ok {
		data, encodeErr := json.Marshal(findings)
		var diagnostics []json.RawMessage
		if encodeErr == nil && json.Unmarshal(data, &diagnostics) == nil && len(diagnostics) > 0 {
			status = StatusFailed
			// A run whose findings include results it could not finish, such as
			// timed-out mutants, is incomplete rather than a verdict.
			if incomplete, _ := failure.Extensions["levenshteinIncomplete"].(bool); incomplete {
				status = StatusIncomplete
			}
			summary, _ := failure.Extensions["levenshteinSummary"].(string)
			details, _ = json.Marshal(struct {
				Findings []json.RawMessage `json:"findings"`
				Summary  json.RawMessage   `json:"summary,omitempty"`
			}{diagnostics, rawJSON(summary)})
		}
	}

	return Result{Status: status, Error: err.Error(), Stdout: stdout, Stderr: stderr, Details: details}
}

// Generate freshness outside Dagger's cached function invocation.
func executionNonce(req Request) string {
	if req.RerunChecks || alwaysFresh[req.Check.Kind] != "" {
		return rand.Text()
	}
	return ""
}
