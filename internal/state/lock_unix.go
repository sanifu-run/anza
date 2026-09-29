//go:build !windows

package state

// Unix locks are advisory flock locks on persistent per-user files. Kernel
// release on process exit provides the recovery signal; timestamps never steal
// a live process's lock.
