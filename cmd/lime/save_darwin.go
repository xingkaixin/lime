package main

import (
	"errors"
	"os"
	"syscall"

	"github.com/ebitengine/purego"
)

func init() {
	var fcopyfile func(int32, int32, uintptr, uint32) int32
	purego.RegisterLibFunc(&fcopyfile, purego.RTLD_DEFAULT, "fcopyfile")
	copySaveACL = func(src, dst *os.File) error {
		const copyfileACL = 1
		if fcopyfile(int32(src.Fd()), int32(dst.Fd()), 0, copyfileACL) != 0 {
			return errors.New("could not preserve file ACL")
		}
		return nil
	}
	caseInsensitiveDir = func(dir string) bool {
		const pcCaseSensitive = 11
		sensitive, err := syscall.Pathconf(dir, pcCaseSensitive)
		return err == nil && sensitive == 0
	}
}
