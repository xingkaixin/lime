package main

import (
	"golang.org/x/sys/unix"
	"os"
)

func init() { copySaveACL = copyLinuxACL }

func copyLinuxACL(src, dst *os.File) error {
	const acl = "system.posix_acl_access"
	srcFD, dstFD := int(src.Fd()), int(dst.Fd())
	size, err := unix.Fgetxattr(srcFD, acl, nil)
	switch err {
	case unix.ENOTSUP:
		return nil
	case unix.ENODATA:
		// The temporary file may have inherited an ACL from its directory.
		err = unix.Fremovexattr(dstFD, acl)
		if err == unix.ENODATA || err == unix.ENOTSUP {
			return nil
		}
		return err
	case nil:
	default:
		return err
	}
	buf := make([]byte, size)
	n, err := unix.Fgetxattr(srcFD, acl, buf)
	if err != nil {
		return err
	}
	return unix.Fsetxattr(dstFD, acl, buf[:n], 0)
}
