package verify

import "sync"

// Session is what one verification run remembers while its checks execute:
// the file stat memo, the git listing of each source, the shared checkout's
// snapshot, and the host Go toolchain's identity. Each answer holds for as
// long as the run does, and no longer, so a process that verifies again, such
// as a watch loop or an editor integration, starts a new session and sees the
// tree, the shared checkout and the toolchain as they are then.
//
// Execute gives every Request the session it runs in. A Request built any
// other way, as a test builds one, runs in a new session per call (see
// sessionOf), which remembers nothing from one call to the next.
type Session struct {
	// stats is the file stat memo (statcache.go), persisted under the cache
	// directory the session was opened with.
	stats *statStore
	// implementations memoizes the shared checkout's snapshot per
	// implementationKey (see implementation).
	implementations sync.Map
	// listings memoizes one git listing per source (see listing).
	listings sync.Map
	// listingMu orders relists against memoizing a listing: listingEpochs
	// counts each source's relists, so a listing git produced before a relist
	// is never memoized after it.
	listingMu     sync.Mutex
	listingEpochs map[string]uint64
	// listingFetched, when set, is called after git lists a source and before
	// the listing is memoized. Tests use it to change the tree in between.
	listingFetched func(source string)
	// toolchains memoizes one Go toolchain identity per resolved go binary and
	// environment (see toolchainIdentity).
	toolchains sync.Map
	// workspaces coordinates native checks inside their working trees. Every
	// session in a process shares processWorkspaces; gate.go says why.
	workspaces *workspaces
	// waiting, when set, is told the name of every lock a check in this session
	// finds held and starts waiting for. Tests use it to act while a check is
	// known to be blocked.
	waiting func(name string)
}

// NewSession opens a session for one run. The stat memo persists under the
// cache's directory; without a cache it lasts only as long as the session.
func NewSession(cache *Cache) *Session {
	if cache == nil {
		return newSession("", recordLimit)
	}
	return newSession(cache.Dir, cache.maxRecord())
}

func newSession(dir string, limit int) *Session {
	return &Session{stats: newStatStore(dir, limit), listingEpochs: map[string]uint64{}, workspaces: processWorkspaces}
}

// Flush persists the file stat memo beside the result records, so the next run
// can skip rereading files it has already hashed. The memo is a hint that
// every lookup revalidates, so a failure here costs speed on the next run and
// never correctness.
func (s *Session) Flush() error {
	return s.stats.flush()
}

// sessionOf is the session req runs in. A request Execute did not build gets a
// new one each time, so nothing is remembered between calls.
func sessionOf(req Request) *Session {
	if req.session != nil {
		return req.session
	}
	return newSession("", recordLimit)
}
