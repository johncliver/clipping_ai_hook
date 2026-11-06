//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// init activa ENABLE_VIRTUAL_TERMINAL_PROCESSING para que cmd/PowerShell
// rendericen los códigos ANSI. Windows Terminal ya lo trae por defecto,
// pero esto también cubre consolas clásicas.
func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	const stdOutputHandle = ^uintptr(10) // (DWORD)-11

	h, _, _ := getStdHandle.Call(stdOutputHandle)
	if h == 0 || h == ^uintptr(0) {
		return
	}
	var mode uint32
	ok, _, _ := getConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	if ok == 0 {
		return
	}
	const enableVirtualTerminalProcessing = 0x0004
	mode |= enableVirtualTerminalProcessing
	setConsoleMode.Call(h, uintptr(mode))
}
