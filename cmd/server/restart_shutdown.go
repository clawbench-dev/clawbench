package main

import (
	"sync"
)

// requestShutdown asks the shutdown goroutine to begin a graceful shutdown.
//
// On Unix this is not the mechanism used (a self-delivered SIGINT is, see
// restart_unix.go); on Windows a signal cannot be delivered to oneself at all,
// so a channel is the only portable way. Both paths are safe to call more than
// once: shutdown must be idempotent, because a restart request racing a real
// Ctrl+C would otherwise close the channel twice and panic.
var (
	shutdownOnce     sync.Once
	shutdownRequests = make(chan struct{})
)

func requestShutdown() {
	shutdownOnce.Do(func() { close(shutdownRequests) })
}
