//go:build unix

package config

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwner refuses a file owned by someone who could rewrite what FleetDesk
// is about to trust or execute. config.yaml must be the user's own; a fleet
// file may also be root's, since only root can then change it.
func checkOwner(info os.FileInfo, path string, allowRoot bool) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	uid := os.Getuid()
	if int(st.Uid) == uid || (allowRoot && st.Uid == 0) {
		return nil
	}
	return fmt.Errorf("%s is owned by uid %d, not by you (uid %d); FleetDesk will not use a file another user can rewrite — run: chown %d %s", path, st.Uid, uid, uid, path)
}
