//go:build !windows

package xfetch

import "syscall"

func sysProcAttrDetach() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
