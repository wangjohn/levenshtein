package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type CacheInfo struct {
	Status   string `json:"status"`
	Key      string `json:"key,omitempty"`
	Reason   string `json:"reason,omitempty"`
	LookupMS int64  `json:"lookup_ms"`
}
type StageResult struct {
	Kind       string `json:"kind"`
	Key        string `json:"key"`
	Reused     bool   `json:"reused"`
	DurationMS int64  `json:"duration_ms"`
}
type Cache struct{ Dir string }
type CachedExecutor struct {
	Cache    *Cache
	Executor Executor
}
type artifact struct {
	Path string
	Mode uint32
	Data []byte
}
type entry struct {
	Key       string
	Result    Result
	Artifacts []artifact
}
type envelope struct {
	Checksum string
	Data     json.RawMessage
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func writeRecord(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope{Checksum: digest(json.RawMessage(data)), Data: data})
	if err != nil {
		return err
	}
	return atomicWrite(path, body, 0600)
}
func readRecord(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil {
		return err
	}
	var e envelope
	if err = json.Unmarshal(data, &e); err != nil {
		return err
	}
	if e.Checksum != digest(e.Data) {
		return fmt.Errorf("cache checksum mismatch")
	}
	return json.Unmarshal(e.Data, value)
}
func safeArtifact(root, path string) (string, error) {
	if !relative(path) || path == "." {
		return "", fmt.Errorf("invalid artifact path %q", path)
	}
	full := filepath.Join(root, path)
	for current := full; current != root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("artifact path %q contains a symlink", path)
		}
	}
	return full, nil
}
func (c *Cache) load(req Request, key string) (Result, error) {
	var e entry
	if err := readRecord(filepath.Join(c.Dir, "results", key+".json"), &e); err != nil {
		return Result{}, err
	}
	if e.Key != key || e.Result.Status != "passed" || e.Result.VerifiedAt.IsZero() || len(e.Artifacts) != len(req.Check.Artifacts) {
		return Result{}, fmt.Errorf("incomplete cached result")
	}
	// Validate every output destination before restoring any artifacts.
	for i, a := range e.Artifacts {
		if a.Path != req.Check.Artifacts[i] {
			return Result{}, fmt.Errorf("artifact scope mismatch")
		}
		if _, err := safeArtifact(req.Source, a.Path); err != nil {
			return Result{}, err
		}
	}
	for _, a := range e.Artifacts {
		full, _ := safeArtifact(req.Source, a.Path)
		if err := atomicWrite(full, a.Data, os.FileMode(a.Mode)&0777); err != nil {
			return Result{}, err
		}
	}
	return e.Result, nil
}
func (c *Cache) save(req Request, key string, result Result) error {
	e := entry{Key: key, Result: result}
	for _, path := range req.Check.Artifacts {
		full, err := safeArtifact(req.Source, path)
		if err != nil {
			return err
		}
		info, err := os.Stat(full)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact is not a regular file")
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		e.Artifacts = append(e.Artifacts, artifact{Path: path, Mode: uint32(info.Mode().Perm()), Data: data})
	}
	return writeRecord(filepath.Join(c.Dir, "results", key+".json"), e)
}
func (c CachedExecutor) Execute(ctx context.Context, req Request) Result {
	start := time.Now()
	if ctx.Err() != nil {
		return Result{Status: "cancelled", Error: ctx.Err().Error()}
	}
	if c.Cache != nil && req.Environment.Executor == "native" {
		unlock, err := lockFile(ctx, filepath.Join(c.Cache.Dir, "locks", "workspace-"+digest(req.Source)))
		if err != nil {
			return Result{Status: "error", Error: "cannot lock native workspace: " + err.Error()}
		}
		defer unlock()
	}
	info := CacheInfo{Status: "disabled"}
	eligible := req.Check.Cache || req.Environment.Executor == "dagger"
	if c.Cache == nil || !eligible {
		result := c.Executor.Execute(ctx, req)
		result.Cache = info
		return result
	}
	key, unlock, err := c.Cache.lockedFingerprint(ctx, req)
	if err != nil {
		result := c.Executor.Execute(ctx, req)
		result.Cache = CacheInfo{Status: "unavailable", Reason: err.Error()}
		return result
	}
	info.Key = key
	info.Status = "miss"
	defer unlock()
	retryPath := filepath.Join(c.Cache.Dir, "results", key+".retry")
	if _, err := os.Stat(retryPath); err == nil {
		req.Fresh = true
		info.Reason = "previous execution did not publish a successful result; bypassing underlying verdict caches"
	}
	if !req.Fresh {
		if result, err := c.Cache.load(req, key); err == nil {
			after, changedErr := fingerprint(req)
			if changedErr == nil && after == key {
				info.Status = "hit"
				info.LookupMS = time.Since(start).Milliseconds()
				result.Cache = info
				return result
			}
		}
	} else {
		info.Status = "fresh"
	}
	// A new observation supersedes an older success, including failure/cancellation.
	if err := os.Remove(filepath.Join(c.Cache.Dir, "results", key+".json")); err != nil && !os.IsNotExist(err) {
		return Result{Status: "error", Error: "cannot invalidate old cached result: " + err.Error()}
	}
	if err := atomicWrite(retryPath, []byte("verification pending\n"), 0600); err != nil {
		return Result{Status: "error", Error: "cannot record verification freshness: " + err.Error()}
	}
	info.LookupMS = time.Since(start).Milliseconds()
	executed := time.Now()
	result := c.Executor.Execute(ctx, req)
	result.VerifiedAt = executed.UTC()
	result.ExecutionMS = time.Since(executed).Milliseconds()
	result.Cache = info
	if result.Status == "passed" {
		after, err := fingerprint(req)
		if err != nil || after != key {
			result.Cache.Reason = "inputs changed during execution; result was not cached"
			return result
		}
		if err := c.Cache.save(req, key, result); err != nil {
			result.Cache.Reason = "cache write unavailable: " + err.Error()
		} else {
			if err := os.Remove(retryPath); err != nil {
				result.Cache.Reason = "freshness marker cleanup unavailable: " + err.Error()
			}
		}
	}
	return result
}

func (c *Cache) lockedFingerprint(ctx context.Context, req Request) (string, func(), error) {
	for attempt := 0; attempt < 3; attempt++ {
		key, err := fingerprint(req)
		if err != nil {
			return "", nil, err
		}
		unlock, err := lockFile(ctx, filepath.Join(c.Dir, "locks", "result-"+key))
		if err != nil {
			return "", nil, err
		}
		current, err := fingerprint(req)
		if err == nil && current == key {
			return key, unlock, nil
		}
		unlock()
		if err != nil {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("inputs kept changing during cache lookup")
}
