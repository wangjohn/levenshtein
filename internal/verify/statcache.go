package verify

import (
	"io/fs"
	"os"
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

// statEntry pairs an observed stat with the snapshot text that content produced
// and the moment the content was read. HashedAt, not the time the record was
// written, is what decides whether the entry was racy: the hash may have been
// taken long before the process flushed it.
type statEntry struct {
	Stat     fileStat `json:"stat"`
	Value    string   `json:"value"`
	HashedAt int64    `json:"hashed_at_ns"`
}

// statRecord is one root's persisted memo.
type statRecord struct {
	Root    string               `json:"root"`
	Entries map[string]statEntry `json:"entries"`
}

// racyWindow is git's own racy-index allowance: a file whose modification time
// is within this of the moment its content was hashed may have been rewritten in
// the same filesystem timestamp tick, so a persisted entry for it is never
// trusted. A record written before HashedAt existed decodes it as zero and so
// trusts nothing.
const racyWindow = 2 * time.Second

// settled reports whether an entry's content was read long enough after the
// file's last modification that a later same-tick write is impossible.
func (e statEntry) settled() bool {
	return e.Stat.ModNS < e.HashedAt-int64(racyWindow)
}

// statStore is the process-wide memo. dir is the cache directory the records
// live under; without one the memo still works, it just does not survive the
// process. seen marks the entries this run looked up or stored, so a flush can
// drop the ones for files that no longer exist.
type statStore struct {
	mu      sync.Mutex
	dir     string
	entries map[statKey]statEntry
	seen    map[statKey]bool
	roots   map[string]bool
}

var stats = &statStore{entries: map[statKey]statEntry{}, seen: map[statKey]bool{}, roots: map[string]bool{}}

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
	s.seen = map[statKey]bool{}
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
// saved, and an entry that was racy when it was hashed is dropped.
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
	for name, entry := range record.Entries {
		if entry.settled() {
			s.entries[statKey{Root: root, Path: name}] = entry
		}
	}
}

func (s *statStore) lookup(key statKey, current fileStat) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.Stat != current {
		return "", false
	}
	s.seen[key] = true
	return entry.Value, true
}

func (s *statStore) store(key statKey, current fileStat, value string, hashedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[key] = statEntry{Stat: current, Value: value, HashedAt: hashedAt.UnixNano()}
	s.seen[key] = true
}

// flush writes one record per root the process touched. An entry this run did
// not look at is kept only while its file still has the recorded stat, so a
// record does not accumulate deleted or rewritten files, yet a run that covers
// one target does not discard the hints another target's run left. The record
// is a hint for the next run, so a write failure is reported but never
// invalidates anything.
func (s *statStore) flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.dir == "" {
		return nil
	}
	// Every touched root is rewritten, even when nothing survives, so a record
	// cannot outlive the files it described.
	grouped := map[string]map[string]statEntry{}
	for root := range s.roots {
		grouped[root] = map[string]statEntry{}
	}
	opened := map[string]*os.Root{}
	defer func() {
		for _, dir := range opened {
			_ = dir.Close() // Directory handle cleanup; nothing is written through it.
		}
	}()
	for key, entry := range s.entries {
		if !s.roots[key.Root] {
			continue
		}
		if !s.seen[key] && !unchanged(opened, key, entry.Stat) {
			continue
		}
		grouped[key.Root][key.Path] = entry
	}

	var failure error
	for root, entries := range grouped {
		record := statRecord{Root: root, Entries: entries}
		if err := writeRecord(s.path(root), record); err != nil && failure == nil {
			failure = err
		}
	}
	return failure
}

// unchanged reports whether a file still has the stat an entry recorded. Each
// root is opened once per flush and cached in opened.
func unchanged(opened map[string]*os.Root, key statKey, recorded fileStat) bool {
	dir, ok := opened[key.Root]
	if !ok {
		var err error
		if dir, err = os.OpenRoot(key.Root); err != nil {
			return false
		}
		opened[key.Root] = dir
	}

	info, err := dir.Lstat(key.Path)
	return err == nil && info.Mode().IsRegular() && statOf(info) == recorded
}
