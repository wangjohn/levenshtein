package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CacheInfo struct {
	Status   CacheStatus `json:"status"`
	Key      string      `json:"key,omitempty"`
	Reason   string      `json:"reason,omitempty"`
	LookupMS int64       `json:"lookup_ms"`
}

type StageResult struct {
	Kind       StageKind `json:"kind"`
	Key        string    `json:"key"`
	Reused     bool      `json:"reused"`
	DurationMS int64     `json:"duration_ms"`
}

type Cache struct{ Dir string }

// Flush persists the process-wide file stat memo beside the result records, so
// the next run can skip rereading files it has already hashed. The memo is a
// hint that every lookup revalidates, so a failure here costs speed on the next
// run and never correctness.
func (c *Cache) Flush() error {
	if c == nil {
		return nil
	}
	stats.configure(c.Dir)
	return stats.flush()
}

// notes joins the reasons a result carries, so a discovery fallback does not
// hide a freshness marker or the other way around.
func notes(values ...string) string {
	var present []string
	for _, value := range values {
		if value != "" {
			present = append(present, value)
		}
	}
	return strings.Join(present, "; ")
}

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

// envelope is the on-disk record layout. The tags spell the field names that
// untagged records already used, so existing cache files still decode.
type envelope struct {
	Checksum string          `json:"Checksum"`
	Data     json.RawMessage `json:"Data"`
}

// recordLimit is the largest record, a result or a stat memo shard, that is
// written or read. A write over it is refused rather than left for every later
// read to reject. It is a variable so tests can reach the boundary cheaply.
var recordLimit = 64 << 20

func writeRecord(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope{Checksum: digest(json.RawMessage(data)), Data: data})
	if err != nil {
		return err
	}
	if len(body) > recordLimit {
		return fmt.Errorf("record of %d bytes is over the %d-byte cache record limit", len(body), recordLimit)
	}
	return atomicWrite(path, body, 0600)
}

func readRecord(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // Read-only file cleanup.

	data, err := io.ReadAll(io.LimitReader(f, int64(recordLimit)+1))
	if err != nil {
		return err
	}
	if len(data) > recordLimit {
		return fmt.Errorf("record is over the %d-byte cache record limit", recordLimit)
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

func (c *Cache) load(req Request, key string) (Result, error) {
	var e entry
	if err := readRecord(filepath.Join(c.Dir, "results", key+".json"), &e); err != nil {
		return Result{}, err
	}
	if e.Key != key || e.Result.Status != StatusPassed || e.Result.VerifiedAt.IsZero() || len(e.Artifacts) != len(req.Check.artifacts()) {
		return Result{}, fmt.Errorf("incomplete cached result")
	}

	// Validate every output destination before restoring any artifacts.
	for i, a := range e.Artifacts {
		if a.Path != req.Check.artifacts()[i] {
			return Result{}, fmt.Errorf("artifact scope mismatch")
		}
		if err := checkOutputPath(req.Source, a.Path); err != nil {
			return Result{}, err
		}
	}

	root, err := os.OpenRoot(req.Source)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup; writes are closed separately.

	for _, a := range e.Artifacts {
		if err := atomicWriteRoot(root, a.Path, a.Data, os.FileMode(a.Mode)&0777); err != nil {
			return Result{}, err
		}
	}
	return e.Result, nil
}

func (c *Cache) save(req Request, key string, result Result) error {
	e := entry{Key: key, Result: result}

	root, err := os.OpenRoot(req.Source)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup; writes are closed separately.
	for _, path := range req.Check.artifacts() {
		if err := checkOutputPath(req.Source, path); err != nil {
			return err
		}
		info, err := root.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact is not a regular file")
		}
		data, err := root.ReadFile(path)
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
		return Result{Status: StatusCancelled, Error: ctx.Err().Error()}
	}

	// Every snapshot happens inside a check, so this is where the process-wide
	// stat memo learns which cache directory it persists to.
	if c.Cache != nil {
		stats.configure(c.Cache.Dir)
	}

	// Some verdicts depend on state that changes independently of source
	// fingerprints. Never reuse one, even when the consumer enables result
	// caching.
	if reason := alwaysFreshReason(req.Check.Kind); reason != "" {
		req.RerunChecks = true
		result := c.run(ctx, req)
		return result.withCache(CacheInfo{Status: CacheDisabled, Reason: reason})
	}

	// The files a go-mutation run mutates, and the API go-apidiff compares
	// with, depend on where the base branch points, which no input fingerprint
	// sees. Dagger still reuses a run whose arguments are all unchanged, so a
	// repeat costs little.
	if reason := baseDependentReason(req.Check.Kind); reason != "" {
		result := c.run(ctx, req)
		return result.withCache(CacheInfo{Status: CacheDisabled, Reason: reason})
	}

	// A shared Go check is cacheable for the same reason a Dagger one is: its
	// inputs and its tooling are both identified, whichever executor runs it.
	eligible := req.Check.cacheable() || req.Environment.Executor == ExecutorDagger || sharedGoCheck(req.Check.Kind)
	if c.Cache == nil || !eligible {
		result := c.run(ctx, req)
		return result.withCache(CacheInfo{Status: CacheDisabled})
	}

	// A native check enters its working tree only when it has to: to execute,
	// or to restore artifacts on a hit. A hit with nothing to restore reads
	// only the cache, so it neither waits for a running command check nor
	// holds one up. Starting without the workspace and then finding it must
	// execute costs one more lookup, taken with the workspace held.
	native := req.Environment.Executor == ExecutorNative
	gated := native && (req.RerunChecks || len(req.Check.artifacts()) > 0)
	for {
		key, release, err := c.Cache.lockedFingerprint(ctx, req, gated)
		if err != nil {
			if ctx.Err() != nil {
				return Result{Status: StatusCancelled, Error: ctx.Err().Error()}
			}
			result := c.run(ctx, req)
			return result.withCache(CacheInfo{Status: CacheUnavailable, Reason: err.Error()})
		}

		attempt := req
		retryPath := filepath.Join(c.Cache.Dir, "results", key+".retry")
		reason := discoveryNote(ctx, req.Source, req.Target.Discovery)
		if _, err := os.Stat(retryPath); err == nil {
			attempt.RerunChecks = true
			reason = notes(reason, "previous execution did not publish a successful result; bypassing underlying verdict caches")
		}
		if !attempt.RerunChecks {
			// The key was confirmed with the result lock held, and a record
			// changes only under that lock, so what loads is the verdict for
			// the inputs as they are now.
			if result, err := c.Cache.load(attempt, key); err == nil {
				release()
				return result.withCache(CacheInfo{Status: CacheHit, Key: key, Reason: reason, LookupMS: time.Since(start).Milliseconds()})
			}
		}
		if gated || !native {
			defer release()
			return c.execute(ctx, attempt, key, reason, start)
		}
		release()
		gated = true
	}
}

// execute runs a check under its held result lock, and workspace for a native
// check, and records a success under key.
func (c CachedExecutor) execute(ctx context.Context, req Request, key, reason string, start time.Time) Result {
	status := CacheMiss
	if req.RerunChecks {
		status = CacheFresh
	}
	retryPath := filepath.Join(c.Cache.Dir, "results", key+".retry")

	// A new observation supersedes an older success, including failure/cancellation.
	if err := os.Remove(filepath.Join(c.Cache.Dir, "results", key+".json")); err != nil && !os.IsNotExist(err) {
		return Result{Status: StatusError, Error: "cannot invalidate old cached result: " + err.Error()}
	}
	if err := atomicWrite(retryPath, []byte("verification pending\n"), 0600); err != nil {
		return Result{Status: StatusError, Error: "cannot record verification freshness: " + err.Error()}
	}
	lookupMS := time.Since(start).Milliseconds()
	executed := time.Now()
	outcome := c.executeOnce(ctx, req)
	result := Result{
		ID:          outcome.ID,
		Status:      outcome.Status,
		DurationMS:  outcome.DurationMS,
		VerifiedAt:  executed.UTC(),
		Stdout:      outcome.Stdout,
		Stderr:      outcome.Stderr,
		Error:       outcome.Error,
		Cache:       CacheInfo{Status: status, Key: key, Reason: reason, LookupMS: lookupMS},
		ExecutionMS: time.Since(executed).Milliseconds(),
		Stages:      outcome.Stages,
		Details:     outcome.Details,
		Warnings:    outcome.Warnings,
	}

	if result.Status == StatusPassed {
		after, err := fingerprint(ctx, req)
		if err != nil || after != key {
			return result.withCache(CacheInfo{Status: status, Key: key, Reason: notes(reason, "inputs changed during execution; result was not cached"), LookupMS: lookupMS})
		}
		if err := c.Cache.save(req, key, result); err != nil {
			reason = notes(reason, "cache write unavailable: "+err.Error())
		} else {
			if err := os.Remove(retryPath); err != nil {
				reason = notes(reason, "freshness marker cleanup unavailable: "+err.Error())
			}
		}
	}
	return result.withCache(CacheInfo{Status: status, Key: key, Reason: reason, LookupMS: lookupMS})
}

// run executes a check the cache does not hold, entering the working tree
// first when the check is native.
func (c CachedExecutor) run(ctx context.Context, req Request) Result {
	if req.Environment.Executor == ExecutorNative {
		leave, err := acquireWorkspace(ctx, c.cacheDir(), req.Source, writesWorkspace(req))
		if err != nil {
			return workspaceFailure(ctx, err)
		}
		defer leave()
	}
	return c.executeOnce(ctx, req)
}

// executeOnce runs the underlying executor. Whatever the outcome, execution may
// have created files the run's memoized listing predates, and the next
// fingerprint in this run, of this check or any other, must see them. A failed
// command can leave files behind as easily as a passing one.
func (c CachedExecutor) executeOnce(ctx context.Context, req Request) Result {
	result := c.Executor.Execute(ctx, req)
	relist(req.Source)
	return result
}

func (c CachedExecutor) cacheDir() string {
	if c.Cache == nil {
		return ""
	}
	return c.Cache.Dir
}

// workspaceFailure reports a check that never entered its working tree. A
// wait the run's context ended is a cancellation, not a failure to lock.
func workspaceFailure(ctx context.Context, err error) Result {
	if ctx.Err() != nil {
		return Result{Status: StatusCancelled, Error: ctx.Err().Error()}
	}
	return Result{Status: StatusError, Error: "cannot lock native workspace: " + err.Error()}
}

// lockedFingerprint returns the check's key with its result lock held, and,
// when gated, its native workspace too. The inputs are fingerprinted again once
// everything is held: the first fingerprint only names the lock, and whoever
// held that lock or the workspace meanwhile may have changed the inputs, so a
// key that no longer matches is released and looked up again.
func (c *Cache) lockedFingerprint(ctx context.Context, req Request, gated bool) (string, func(), error) {
	for range 3 {
		key, err := fingerprint(ctx, req)
		if err != nil {
			return "", nil, err
		}
		release, err := lockFile(ctx, filepath.Join(c.Dir, "locks", "result-"+key))
		if err != nil {
			return "", nil, err
		}
		if gated {
			unlock := release
			exclusive := writesWorkspace(req) || len(req.Check.artifacts()) > 0
			leave, err := acquireWorkspace(ctx, c.Dir, req.Source, exclusive)
			if err != nil {
				unlock()
				return "", nil, err
			}
			release = func() {
				leave()
				unlock()
			}
		}

		current, err := fingerprint(ctx, req)
		if err == nil && current == key {
			return key, release, nil
		}
		release()
		if err != nil {
			return "", nil, err
		}
	}
	return "", nil, fmt.Errorf("inputs kept changing during cache lookup")
}
