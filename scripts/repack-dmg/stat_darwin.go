//go:build darwin

package main

import (
	"os"
	"syscall"
	"time"
)

type fileMeta struct {
	ino     uint32
	created time.Time
}

func statMeta(path string) (fileMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	st := fi.Sys().(*syscall.Stat_t)
	created := time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	if created.IsZero() || st.Birthtimespec.Sec == 0 {
		created = time.Unix(st.Ctimespec.Sec, st.Ctimespec.Nsec)
	}
	return fileMeta{ino: uint32(st.Ino), created: created}, nil
}
