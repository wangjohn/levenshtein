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

	"dagger.io/dagger"
	"dagger.io/dagger/engineconn"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Dagger shares one SDK session and a stable module across a verification run.
// Consumer sources and fresh-run inputs are function arguments, so they do not
// change the module's download and compiler cache namespace.
type Dagger struct {
	client *dagger.Client
	shared string
}

func (d *Dagger) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// These are the Dagger functions supported by both planning and execution.
var daggerFunctions = map[string]string{"go-lint": "goLint", "self-test": "selfTest"}

func (d *Dagger) Execute(ctx context.Context, req Request) Result {
	result := daggerResult(d.execute(ctx, req))

	if err := ctx.Err(); err != nil {
		result.Status = "cancelled"
		result.Error = err.Error()
	}
	return result
}

func (d *Dagger) execute(ctx context.Context, req Request) error {
	function, ok := daggerFunctions[req.Check.Kind]
	if !ok {
		return fmt.Errorf("unsupported Dagger check %q", req.Check.Kind)
	}

	if err := d.connect(ctx, req.Shared); err != nil {
		return err
	}

	nonce := ""
	if req.Fresh {
		nonce = rand.Text()
	}

	query := d.client.QueryBuilder().Select("levenshtein").Select(function).Arg("nonce", nonce)
	if req.Check.Kind == "go-lint" {
		source := d.client.Host().Directory(req.Source, dagger.HostDirectoryOpts{Exclude: []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git"}})
		query = query.Arg("source", source).Arg("module", req.Target.Dir)
	}
	return query.Execute(ctx)
}

func (d *Dagger) connect(ctx context.Context, shared string) error {
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

	client, err := dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr), dagger.WithSkipWorkspaceModules())
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
		return Result{Status: "passed"}
	}

	result := Result{Status: "error", Error: err.Error()}
	var failure *gqlerror.Error
	if !errors.As(err, &failure) {
		return result
	}

	result.Stdout, _ = failure.Extensions["stdout"].(string)
	result.Stderr, _ = failure.Extensions["stderr"].(string)
	findings, ok := failure.Extensions["levenshteinFindings"]
	if !ok {
		return result
	}

	data, encodeErr := json.Marshal(findings)
	if encodeErr != nil {
		return result
	}
	var diagnostics []json.RawMessage
	if json.Unmarshal(data, &diagnostics) != nil || len(diagnostics) == 0 {
		return result
	}

	result.Status = "failed"
	result.Details, _ = json.Marshal(struct {
		Findings []json.RawMessage `json:"findings"`
	}{diagnostics})
	return result
}
