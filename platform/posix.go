// +build !windows

package platform

import (
	"archive/tar"
	"os"
	"os/exec"
	"strings"
	"syscall"
)
// We have a dummy version of this call in posix.go.
// Windows does not implement the syscall.Stat_t type we
// need, but the *nixes do. We use this in util.AddToArchive
// to set owner/group on files being added to a tar archive.
func GetOwnerAndGroup(finfo os.FileInfo, header *tar.Header) {
	systat := finfo.Sys().(*syscall.Stat_t)
	if systat != nil {
		header.Uid = int(systat.Uid)
		header.Gid = int(systat.Gid)
	}
}

// On Linux and OSX, this uses df in a safe way (without passing
// through any user-supplied input) to find the mountpoint of a
// given file.
func GetMountPointFromPath (path string) (string, error) {
	out, err := exec.Command("df").Output()
	if err != nil {
		return "", err
	}
	matchingMountpoint := ""
	maxPrefixLen := 0
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i > 0 {
			words := strings.Split(line, " ")
			mountpoint := words[len(words) - 1]
			if strings.HasPrefix(path, mountpoint) && len(mountpoint) > maxPrefixLen {
				matchingMountpoint = mountpoint
			}
		}
	}
	return matchingMountpoint, nil
}
