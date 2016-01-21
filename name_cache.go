package continuity

import (
	"os/user"
	"sync"

	"github.com/stevvooe/continuity/group"
)

var (
	cacheLock      sync.RWMutex
	userNameCache  map[string]string
	groupNameCache map[string]string
)

func init() {
	userNameCache = make(map[string]string)
	groupNameCache = make(map[string]string)
}

func lookupUser(name string) (uid string) {
	cacheLock.RLock()
	uid, ok := userNameCache[name]
	if ok {
		cacheLock.RUnlock()
		return
	}
	cacheLock.RUnlock()

	cacheLock.Lock()
	defer cacheLock.Unlock()

	uid, ok = userNameCache[name]
	if ok {
		return
	}

	u, err := user.Lookup(name)
	if err == nil {
		uid = u.Uid
	}
	// TODO(dmcgowan): handle non "user.UnknownUserError"

	userNameCache[name] = uid

	return
}

func lookupGroup(name string) (gid string) {
	cacheLock.RLock()
	gid, ok := groupNameCache[name]
	if ok {
		cacheLock.RUnlock()
		return
	}
	cacheLock.RUnlock()

	cacheLock.Lock()
	defer cacheLock.Unlock()

	gid, ok = groupNameCache[name]
	if ok {
		return
	}

	g, err := group.Lookup(name)
	if err == nil {
		gid = g.Gid
	}
	// TODO(dmcgowan): handle non "group.UnknownGroupError"

	groupNameCache[name] = gid

	return
}
