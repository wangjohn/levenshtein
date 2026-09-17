package bad

import "sync"

func CopyLock(lock sync.Mutex) {
	lock.Lock()
	defer lock.Unlock()
}
