package verify

import (
	"io/fs"
	"path/filepath"
	"sync"
	"time"
)

// Hashing file content dominates cache lookups: fingerprint runs up to four
// times per check, every stage hashes its own inputs again, and overlapping
// targets cover the same files. The stat memo turns those repeats into one
// Lstat comparison per file, shared by every target in the process.
//
// The trade-off this records: within one process a file whose size, modification
// time, inode and mode are all unchanged is assumed to have unchanged content.
// A filesystem with nanosecond timestamps cannot give two different writes the
// same modification time, so only coarse-timestamp filesystems can hide an edit
// from the memo, and only for an edit that also preserves the file's size.
type statKey struct {
	Root string
	Path string
}

// fileStat is the identity an entry is validated against. Inode is zero on
// platforms that do not report one.
type fileStat struct {
	Size  int64  `json:"size"`
	ModNS int64  `json:"mod_ns"`
	Inode uint64 `json:"inode"`
	Mode  uint32 `json:"mode"`
}

// statEntry pairs an observed stat with the snapshot text that content produced.
type statEntry struct {
	Stat  fileStat `json:"stat"`
	Value string   `json:"value"`
}

// statRecord is one root's persisted memo. WrittenAt dates the whole record so
// loading can distrust entries that were still racy when it was written.
type statRecord struct {
	Root      string               `json:"root"`
	WrittenAt time.Time            `json:"written_at"`
	Entries   map[string]statEntry `json:"entries"`
}

// racyWindow is git's own racy-index allowance: a file whose modification time
// is within this of the record's write time may have been rewritten in the same
// filesystem timestamp tick, so a persisted entry for it is never trusted.
const racyWindow = 2 * time.Second

// statStore is the process-wide memo. dir is the cache directory the records
// live under; without one the memo still works, it just does not survive the
// process.
type statStore struct {
	mu      sync.Mutex
	dir     string
	entries map[statKey]statEntry
	roots   map[string]bool
}

var stats = &statStore{entries: map[statKey]statEntry{}, roots: map[string]bool{}}

func statOf(info fs.FileInfo) fileStat {
	return fileStat{
		Size:  info.Size(),
		ModNS: info.ModTime().UnixNano(),
		Inode: inodeOf(info),
		Mode:  uint32(info.Mode().Perm()),
	}
}

// configure points the memo at a cache directory. A different directory is a
// different persisted cache, so the in-memory entries start over with it.
func (s *statStore) configure(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == dir {
		return
	}
	s.dir = dir
	s.entries = map[statKey]statEntry{}
	s.roots = map[string]bool{}
}

func (s *statStore) path(root string) string {
	if s.dir == "" {
		return ""
	}
	return filepath.Join(s.dir, "stat", digest(root)+".json")
}

// prepare loads root's persisted entries the first time the root is snapshotted.
// A missing, corrupt or unreadable record only costs the speed it would have
// saved.
func (s *statStore) prepare(root string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.roots[root] {
		return
	}
	s.roots[root] = true
	path := s.path(root)
	if path == "" {
		return
	}

	var record statRecord
	if err := readRecord(path, &record); err != nil || record.Root != root {
		return
	}
	cutoff := record.WrittenAt.Add(-racyWindow)
	for name, entry := range record.Entries {
		if !time.Unix(0, entry.Stat.ModNS).Before(cutoff) {
			continue
		}
		s.entries[statKey{Root: root, Path: name}] = entry
	}
}

func (s *statStore) lookup(key statKey, current fileStat) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.Stat != current {
		return "", false
	}
	return entry.Value, true
}

func (s *statStore) store(key statKey, current fileStat, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = statEntry{Stat: current, Value: value}
}

// flush writes one record per root the process touched. The record is a hint for
// the next run, so a write failure is reported but never invalidates anything.
func (s *statStore) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return nil
	}
	grouped := map[string]map[string]statEntry{}
	for key, entry := range s.entries {
		if !s.roots[key.Root] {
			continue
		}
		if grouped[key.Root] == nil {
			grouped[key.Root] = map[string]statEntry{}
		}
		grouped[key.Root][key.Path] = entry
	}

	written := time.Now().UTC()
	var failure error
	for root, entries := range grouped {
		record := statRecord{Root: root, WrittenAt: written, Entries: entries}
		if err := writeRecord(s.path(root), record); err != nil && failure == nil {
			failure = err
		}
	}
	return failure
}
