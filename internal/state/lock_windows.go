//go:build windows

package state

// Windows locks are kernel byte-range locks on persistent per-user files.
// Windows releases the lock when the owning process closes or loses its handle.
