package verify

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Hashing file content dominates cache lookups: fingerprint runs at least
// twice per check, every stage hashes its own inputs again, and overlapping
// targets cover the same files. The stat memo turns those repeats into one
// Lstat comparison per file, shared by every target in the session.
//
// The trade-off this records: within one session a file whose size, modification
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
// taken long before the session flushed it.
type statEntry struct {
	Stat     fileStat `json:"stat"`
	Value    string   `json:"value"`
	HashedAt int64    `json:"hashed_at_ns"`
}

// statRecord is one root's persisted memo, or one shard of it. A memo too
// large for one record is split by path hash across Shards records: the first
// at the root's own path, which says how many there are, and the rest beside
// it. Every entry is revalidated on lookup, so records from different flushes
// can mix without harm.
type statRecord struct {
	Root    string               `json:"root"`
	Shards  int                  `json:"shards,omitempty"`
	Entries map[string]statEntry `json:"entries"`
}

// statEntryOverhead approximates the encoded bytes of one entry beyond its
// path and value, for sizing shards before encoding them.
const statEntryOverhead = 128

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

// statStore is one session's memo. dir is the cache directory the records
// live under; without one the memo still works, it just does not survive the
// session. limit is the largest record it writes or reads. seen marks the
// entries this run looked up or stored, so a flush can drop the ones for files
// that no longer exist.
type statStore struct {
	mu      sync.Mutex
	dir     string
	limit   int
	entries map[statKey]statEntry
	seen    map[statKey]bool
	roots   map[string]bool
}

func newStatStore(dir string, limit int) *statStore {
	return &statStore{dir: dir, limit: limit, entries: map[statKey]statEntry{}, seen: map[statKey]bool{}, roots: map[string]bool{}}
}

func statOf(info fs.FileInfo) fileStat {
	return fileStat{
		Size:  info.Size(),
		ModNS: info.ModTime().UnixNano(),
		Inode: inodeOf(info),
		Mode:  uint32(info.Mode().Perm()),
	}
}

// path is where shard 0 of root's memo lives; shard is where shard i does.
func (s *statStore) path(root string) string {
	return s.shard(root, 0)
}

func (s *statStore) shard(root string, i int) string {
	if s.dir == "" {
		return ""
	}
	if i == 0 {
		return filepath.Join(s.dir, "stat", digest(root)+".json")
	}
	return filepath.Join(s.dir, "stat", fmt.Sprintf("%s-%d.json", digest(root), i))
}

// shards is how many records keep each under half the record limit, which
// leaves room for paths longer than the estimate assumes.
func shards(entries map[string]statEntry, limit int) int {
	size := 0
	for name, entry := range entries {
		size += len(name) + len(entry.Value) + statEntryOverhead
	}
	count := 1
	for size/count > limit/2 {
		count *= 2
	}
	return count
}

func shardOf(name string, count int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name)) // Writing to a hash never fails.
	return int(h.Sum32() % uint32(count))
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
	if err := readRecord(path, &record, s.limit); err != nil || record.Root != root {
		return
	}
	s.load(root, record)
	for i := 1; i < record.Shards; i++ {
		var shard statRecord
		if err := readRecord(s.shard(root, i), &shard, s.limit); err == nil && shard.Root == root {
			s.load(root, shard)
		}
	}
}

// load keeps a record's settled entries. The caller holds s.mu.
func (s *statStore) load(root string, record statRecord) {
	for name, entry := range record.Entries {
		if entry.settled() {
			s.entries[statKey{Root: root, Path: name}] = entry
		}
	}
}

func (s *statStore) lookup(key statKey, current fileStat) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// A racy entry is no more trustworthy in memory than on disk: on a
	// filesystem with coarse timestamps, a same-size rewrite in the tick the
	// content was hashed in leaves the stat unchanged.
	entry, ok := s.entries[key]
	if !ok || entry.Stat != current || !entry.settled() {
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

// flush writes one record per root the session touched. An entry this run did
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
		if err := s.write(root, entries); err != nil && failure == nil {
			failure = err
		}
	}
	return failure
}

// write persists one root's entries across as many shards as their size
// needs, then removes shards a larger earlier memo left beyond them. Shard 0
// is written last, so a reader never follows its count to a shard this flush
// has yet to write.
func (s *statStore) write(root string, entries map[string]statEntry) error {
	count := shards(entries, s.limit)
	split := make([]map[string]statEntry, count)
	for i := range split {
		split[i] = map[string]statEntry{}
	}
	for name, entry := range entries {
		split[shardOf(name, count)][name] = entry
	}

	var failure error
	for i := count - 1; i >= 0; i-- {
		shard := 0
		if i == 0 && count > 1 {
			shard = count
		}
		if err := writeRecord(s.shard(root, i), statRecord{Root: root, Shards: shard, Entries: split[i]}, s.limit); err != nil && failure == nil {
			failure = err
		}
	}

	stale, _ := filepath.Glob(filepath.Join(s.dir, "stat", digest(root)+"-*.json"))
	for _, path := range stale {
		var i int
		if _, err := fmt.Sscanf(filepath.Base(path), digest(root)+"-%d.json", &i); err == nil && i >= count {
			_ = os.Remove(path) // A leftover shard is only a hint nobody reads.
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
