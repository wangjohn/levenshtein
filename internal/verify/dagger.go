package verify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Dagger struct{}

func (Dagger) Execute(ctx context.Context, req Request) Result {
	r := Result{Status: "error"}
	version, err := os.ReadFile(filepath.Join(req.Shared, ".dagger-version"))
	if err != nil {
		r.Error = err.Error()
		return r
	}
	actual, err := exec.CommandContext(ctx, "dagger", "version").Output()
	if err != nil {
		r.Error = fmt.Sprintf("Dagger %s is required: %v", strings.TrimSpace(string(version)), err)
		return r
	}
	if !strings.HasPrefix(string(actual), "dagger v"+strings.TrimSpace(string(version))+" ") {
		r.Error = "Dagger version does not match .dagger-version"
		return r
	}
	args := []string{"--mod", req.Shared, "call", "check", "--source", req.Source, "--module", req.Target.Dir, "--check", req.Check.Kind}
	if req.Fresh {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			r.Error = err.Error()
			return r
		}
		args = append(args, "--nonce", hex.EncodeToString(nonce))
	}
	cmd := exec.CommandContext(ctx, "dagger", args...)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	r.Stderr = stderr.String()
	if ctx.Err() != nil {
		r.Status = "cancelled"
		r.Error = ctx.Err().Error()
		return r
	}
	if err != nil {
		r.Error = err.Error()
		r.Stdout = out.String()
		return r
	}
	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		r.Error = fmt.Sprintf("invalid Dagger result: %v", err)
		r.Stdout = out.String()
		return r
	}
	if response.Status != "passed" && response.Status != "failed" && response.Status != "error" {
		r.Error = "incomplete Dagger result"
		return r
	}
	r.Status = response.Status
	r.Details = append(json.RawMessage(nil), out.Bytes()...)
	return r
}
