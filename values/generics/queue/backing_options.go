package queue

import (
	"fmt"
	"os"
	"time"
)

// BackingOption is an optional argument to a backing constructor. A single option type is
// shared across constructors; each option validates against the constructor it is given
// (backingOpts.call) and returns an error if it is not valid there.
type BackingOption func(backingOpts) (backingOpts, error)

// backingCall identifies which backing constructor a BackingOption is being applied to,
// so a single shared option type can reject use with a constructor it does not support.
type backingCall uint8

const (
	unknownCall       backingCall = 0
	callBTreeFIFO     backingCall = 1 // NewBTreeFIFO
	callBTreePriority backingCall = 2 // NewBTreePriority
	callBboltFIFO     backingCall = 3 // NewBboltFIFO
	callBboltPriority backingCall = 4 // NewBboltPriority
)

// backingOpts is the construction-time settings shared by the backing constructors. call
// records which constructor is applying the options so each option can validate it.
type backingOpts struct {
	call  backingCall
	width int
	index bool

	// bbolt-only settings, applied to bolt.Options at open.
	boltNoSync          bool
	boltNoFreelistSync  bool
	boltNoGrowSync      bool
	boltPreLoadFreelist bool
	boltFreelistMap     bool
	boltMlock           bool
	boltMmapFlags       int
	boltInitialMmapSize int
	boltPageSize        int
	boltTimeout         time.Duration
	boltOpenFile        func(string, int, os.FileMode) (*os.File, error)
}

// bboltOnly returns an error if o.call is not a bbolt constructor; used by the
// bbolt-specific options.
func (o backingOpts) bboltOnly(name string) error {
	switch o.call {
	case callBboltFIFO, callBboltPriority:
		return nil
	default:
		return fmt.Errorf("%s is only valid for the bbolt backings (NewBboltFIFO/NewBboltPriority)", name)
	}
}

// WithIndex keeps an in-memory index of items keyed by Item.Hash so Exists and Del do a
// bucket lookup instead of a full scan. This trades extra memory and a little push/pop
// overhead for much faster Exists/Del on delete-heavy workloads (e.g. scanning RangeAll
// and deleting each matching entry). Valid for NewBTreeFIFO (switches it from the
// positional btype tree to a keyed B-Tree, the positional FIFO having no stable locator
// to index), NewBTreePriority, NewBboltFIFO and NewBboltPriority.
func WithIndex() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		switch o.call {
		case callBTreeFIFO, callBTreePriority, callBboltFIFO, callBboltPriority:
			o.index = true
		default:
			return o, fmt.Errorf("WithIndex is not valid for this backing")
		}
		return o, nil
	}
}

// WithBTreeWidth sets the keyed B-Tree node width. A larger width means fewer nodes and
// faster access but more memory; smaller is the reverse. Width must be at least 2
// (default 32). Valid only for the keyed B-Tree backings: NewBTreeFIFO (indexed) and
// NewBTreePriority.
func WithBTreeWidth(width int) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		switch o.call {
		case callBTreeFIFO, callBTreePriority:
			o.width = width
		default:
			return o, fmt.Errorf("WithBTreeWidth is not valid for this backing")
		}
		return o, nil
	}
}

// WithNoSync skips the fsync after every bbolt commit (bolt.Options.NoSync). This is much
// faster for bulk loading but is unsafe: a crash or OS failure can lose recently committed
// items. Valid only for NewBboltFIFO and NewBboltPriority.
func WithNoSync() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithNoSync"); err != nil {
			return o, err
		}
		o.boltNoSync = true
		return o, nil
	}
}

// WithNoFreelistSync stops bbolt from syncing its freelist to disk (bolt.Options.
// NoFreelistSync). Faster commits; on the next open the freelist is rebuilt by scanning,
// so it remains crash-safe but reopen is slower. Valid only for NewBboltFIFO and
// NewBboltPriority.
func WithNoFreelistSync() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithNoFreelistSync"); err != nil {
			return o, err
		}
		o.boltNoFreelistSync = true
		return o, nil
	}
}

// WithNoGrowSync skips the fsync after growing the bbolt file (bolt.Options.NoGrowSync).
// Valid only for NewBboltFIFO and NewBboltPriority.
func WithNoGrowSync() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithNoGrowSync"); err != nil {
			return o, err
		}
		o.boltNoGrowSync = true
		return o, nil
	}
}

// WithBoltTimeout sets how long bbolt waits to obtain the database file lock at open
// (bolt.Options.Timeout); zero (default) waits indefinitely. Valid only for NewBboltFIFO
// and NewBboltPriority.
func WithBoltTimeout(d time.Duration) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltTimeout"); err != nil {
			return o, err
		}
		o.boltTimeout = d
		return o, nil
	}
}

// WithBoltPreLoadFreelist loads the free pages when opening the database
// (bolt.Options.PreLoadFreelist): faster first write, slower open. Valid only for
// NewBboltFIFO and NewBboltPriority.
func WithBoltPreLoadFreelist() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltPreLoadFreelist"); err != nil {
			return o, err
		}
		o.boltPreLoadFreelist = true
		return o, nil
	}
}

// WithBoltFreelistMap selects bbolt's hashmap freelist backend instead of the default
// array (bolt.Options.FreelistType). The hashmap backend is faster in almost all cases,
// especially for large/fragmented databases. Valid only for NewBboltFIFO and
// NewBboltPriority.
func WithBoltFreelistMap() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltFreelistMap"); err != nil {
			return o, err
		}
		o.boltFreelistMap = true
		return o, nil
	}
}

// WithBoltMlock locks the database memory-map into RAM to prevent page faults
// (bolt.Options.Mlock, UNIX only); the memory cannot be reclaimed while open. Valid only
// for NewBboltFIFO and NewBboltPriority.
func WithBoltMlock() BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltMlock"); err != nil {
			return o, err
		}
		o.boltMlock = true
		return o, nil
	}
}

// WithBoltMmapFlags sets the flags passed to mmap when memory-mapping the database
// (bolt.Options.MmapFlags), e.g. syscall.MAP_POPULATE on Linux. Valid only for
// NewBboltFIFO and NewBboltPriority.
func WithBoltMmapFlags(flags int) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltMmapFlags"); err != nil {
			return o, err
		}
		o.boltMmapFlags = flags
		return o, nil
	}
}

// WithBoltInitialMmapSize sets the initial mmap size of the database in bytes
// (bolt.Options.InitialMmapSize). A size large enough to hold the database keeps read
// transactions from blocking write transactions. Valid only for NewBboltFIFO and
// NewBboltPriority.
func WithBoltInitialMmapSize(bytes int) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltInitialMmapSize"); err != nil {
			return o, err
		}
		o.boltInitialMmapSize = bytes
		return o, nil
	}
}

// WithBoltPageSize overrides the default OS page size for a newly created database
// (bolt.Options.PageSize). It has no effect on an existing database. Valid only for
// NewBboltFIFO and NewBboltPriority.
func WithBoltPageSize(bytes int) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltPageSize"); err != nil {
			return o, err
		}
		o.boltPageSize = bytes
		return o, nil
	}
}

// WithBoltOpenFile sets the function bbolt uses to open the database file
// (bolt.Options.OpenFile); it defaults to os.OpenFile. Useful for hermetic tests or a
// custom filesystem. Valid only for NewBboltFIFO and NewBboltPriority.
func WithBoltOpenFile(fn func(string, int, os.FileMode) (*os.File, error)) BackingOption {
	return func(o backingOpts) (backingOpts, error) {
		if err := o.bboltOnly("WithBoltOpenFile"); err != nil {
			return o, err
		}
		o.boltOpenFile = fn
		return o, nil
	}
}

// applyBackingOptions seeds backingOpts for the given call and applies options in order,
// returning the first option error.
func applyBackingOptions(call backingCall, options []BackingOption) (backingOpts, error) {
	o := backingOpts{call: call}
	var err error
	for _, opt := range options {
		if o, err = opt(o); err != nil {
			return o, err
		}
	}
	return o, nil
}
