//go:build !darwin

package main

import (
	"errors"
	"time"
)

type fileMeta struct {
	ino     uint32
	created time.Time
}

func statMeta(string) (fileMeta, error) {
	return fileMeta{}, errors.New("repacking DMGs requires macOS")
}
