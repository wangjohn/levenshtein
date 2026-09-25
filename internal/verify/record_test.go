package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lowerRecordLimit shrinks the cache record limit for one test.
func lowerRecordLimit(t *testing.T, limit int) {
	t.Helper()
	previous := recordLimit
	recordLimit = limit
	t.Cleanup(func() { recordLimit = previous })
}

// recordOfSize returns a value whose written record is exactly size bytes.
func recordOfSize(t *testing.T, size int) string {
	t.Helper()
	data, err := json.Marshal("")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := json.Marshal(envelope{Checksum: digest(json.RawMessage(data)), Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Repeat("a", size-len(empty))
}

// A record the reader would refuse is refused when it is written, instead of
// being written and then silently failing every later read.
func TestRecordLimitAppliesToWritesAndReads(t *testing.T) {
	lowerRecordLimit(t, 512)
	path := filepath.Join(t.TempDir(), "record.json")

	if err := writeRecord(path, recordOfSize(t, 512)); err != nil {
		t.Fatalf("a record at the limit was refused: %v", err)
	}
	var value string
	if err := readRecord(path, &value); err != nil || len(value) != len(recordOfSize(t, 512)) {
		t.Fatalf("a record at the limit did not read back: %v", err)
	}

	if err := writeRecord(path, recordOfSize(t, 513)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("a record over the limit was written: %v", err)
	}
	if err := readRecord(path, &value); err != nil {
		t.Fatalf("a refused write replaced the previous record: %v", err)
	}
}

// A result too large to cache is still reported, and the report says why it
// was not cached.
func TestOversizedResultIsReportedAndNotCached(t *testing.T) {
	lowerRecordLimit(t, 1024)
	req := cacheRequest(t)
	executor := &countingExecutor{status: StatusPassed, artifact: "report.txt"}
	req.Check.Command.Artifacts = []string{"report.txt"}
	if err := os.WriteFile(filepath.Join(req.Source, "report.txt"), []byte(strings.Repeat("r", 2048)), 0600); err != nil {
		t.Fatal(err)
	}
	executor.artifact = ""
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

	result := runner.Execute(t.Context(), req)
	if result.Status != StatusPassed || !strings.Contains(result.Cache.Reason, "cache write unavailable") || !strings.Contains(result.Cache.Reason, "limit") {
		t.Fatalf("an oversized result did not say it was not cached: %+v", result.Cache)
	}
}

// The stat memo outgrows one record in a large repository. It is split across
// records that each fit the limit, so every entry survives to the next run.
func TestStatMemoAboveOneRecordSurvivesARestart(t *testing.T) {
	lowerRecordLimit(t, 16<<10)
	dir := t.TempDir()
	root := t.TempDir()
	hashed := time.Now()
	settled := hashed.Add(-time.Hour).UnixNano()
	store := &statStore{entries: map[statKey]statEntry{}, seen: map[statKey]bool{}, roots: map[string]bool{}}
	store.configure(dir)
	store.prepare(root)
	const files = 1000
	for i := range files {
		key := statKey{Root: root, Path: fmt.Sprintf("pkg%02d/file%04d.go", i%50, i)}
		store.store(key, fileStat{Size: int64(i), ModNS: settled, Inode: uint64(i), Mode: 0644}, fmt.Sprintf("644:%064x", i), hashed)
	}
	if err := store.flush(); err != nil {
		t.Fatal(err)
	}

	reloaded := &statStore{entries: map[statKey]statEntry{}, seen: map[statKey]bool{}, roots: map[string]bool{}}
	reloaded.configure(dir)
	reloaded.prepare(root)
	if len(reloaded.entries) != files {
		t.Fatalf("%d of %d stat entries survived a restart", len(reloaded.entries), files)
	}

	// A smaller memo later needs fewer records, and the extra ones go.
	small := &statStore{entries: map[statKey]statEntry{}, seen: map[statKey]bool{}, roots: map[string]bool{}}
	small.configure(dir)
	small.roots[root] = true
	small.store(statKey{Root: root, Path: "only.go"}, fileStat{ModNS: settled}, "644:only", hashed)
	if err := small.flush(); err != nil {
		t.Fatal(err)
	}
	extra, err := filepath.Glob(filepath.Join(dir, "stat", digest(root)+"-*.json"))
	if err != nil || len(extra) != 0 {
		t.Fatalf("stale stat shards were left behind: %v %v", extra, err)
	}
}
