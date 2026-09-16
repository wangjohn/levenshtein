package verify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

func (d *Dagger) Execute(ctx context.Context, req Request) (result Result) {
	result.Status = "error"
	defer func() {
		if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Error = ctx.Err().Error()
		}
	}()
	function := ""
	switch req.Check.Kind {
	case "go-lint":
		function = "goLint"
	case "self-test":
		function = "selfTest"
	default:
		result.Error = fmt.Sprintf("unsupported Dagger check %q", req.Check.Kind)
		return
	}
	version, err := os.ReadFile(filepath.Join(req.Shared, ".dagger-version"))
	if err != nil {
		result.Error = err.Error()
		return
	}
	if strings.TrimSpace(string(version)) != engineconn.CLIVersion {
		result.Error = "Dagger SDK version does not match .dagger-version"
		return
	}
	if d.client == nil {
		d.client, err = dagger.Connect(ctx, dagger.WithLogOutput(os.Stderr), dagger.WithSkipWorkspaceModules())
		if err != nil {
			result.Error = err.Error()
			return
		}
		if err := d.client.ModuleSource(req.Shared).AsModule().Serve(ctx); err != nil {
			_ = d.client.Close()
			d.client = nil
			result.Error = err.Error()
			return
		}
		d.shared = req.Shared
	}
	if d.shared != req.Shared {
		result.Error = "a Dagger session cannot change its shared module"
		return
	}

	nonce := ""
	if req.Fresh {
		value := make([]byte, 16)
		if _, err := rand.Read(value); err != nil {
			result.Error = err.Error()
			return
		}
		nonce = hex.EncodeToString(value)
	}
	query := d.client.QueryBuilder().Select("levenshtein").Select(function).Arg("nonce", nonce)
	if req.Check.Kind == "go-lint" {
		source := d.client.Host().Directory(req.Source, dagger.HostDirectoryOpts{Exclude: []string{"**/.env", "**/.env.*", "!**/.env.example", "**/.git"}})
		query = query.Arg("source", source).Arg("module", req.Target.Dir)
	}
	return daggerResult(query.Execute(ctx))
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
