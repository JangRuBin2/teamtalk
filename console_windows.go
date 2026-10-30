package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableANSI turns on virtual terminal processing so ANSI colors work in cmd/PowerShell.
func enableANSI() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := kernel32.NewProc("GetConsoleMode")
	setMode := kernel32.NewProc("SetConsoleMode")
	const enableVirtualTerminalProcessing = 0x0004
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if r, _, _ := getMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return
	}
	setMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
}

// pauseOnExit keeps the window open when the exe was double-clicked.
const pauseOnExit = true
