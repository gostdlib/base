/*
Package dynamic provides a dynamically configurable select statement.

This is useful when you want to add or remove channels based upon input from users and don't want to spin a
goroutine for each channel.

Cases may be added or removed while a Select() is blocked. There is no polling and no timer: mutators signal an
internal wake channel that causes the blocked select to return and pick up the new case set. An added case becomes
selectable as soon as that happens. A removed case stops being selectable from the same point, but see Remove: it
does not retract a select that is already parked on the previous case set.

Dynamic select statements are not as performant as a static select statement. reflect.Select allocates once per
receive case on every call, so a Select over N channels costs O(N) allocations per call. These shouldn't be needed
often.

Exactly one goroutine may call Select(), TrySelect() or All(). The Add*() and Remove() methods are safe to call
from any goroutine.

Example, receiving from a set of channels that grows while we are selecting:

	s, err := dynamic.New[int]()
	if err != nil {
		// Handle error.
	}

	// Add channels as they arrive from somewhere else. This is safe to do while the
	// loop below is blocked in a select, and takes effect immediately.
	pool := context.Pool(ctx)
	pool.Submit(ctx, func() {
		for recv := range newReceiversCh {
			action := func(ctx context.Context, v int) {
				context.Log(ctx).Info(fmt.Sprintf("received on channel(%d): %d", recv.Num, v))
			}
			if err := s.AddRecv(recv.Ch, action); err != nil {
				context.Log(ctx).Error(err.Error())
			}
		}
	})

	// Drain until the Context is cancelled.
	for r := range s.All(ctx) {
		switch r.Kind {
		case dynamic.Received:
			context.Log(ctx).Info(fmt.Sprintf("got %d", r.Value))
		case dynamic.Closed:
			context.Log(ctx).Info("a channel was closed and removed")
		case dynamic.CtxDone:
			return
		default:
			context.Log(ctx).Error(fmt.Sprintf("unexpected kind %v", r.Kind))
		}
	}
*/
package dynamic

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"sync/atomic"

	"github.com/gostdlib/base/concurrency/sync"
	"github.com/gostdlib/base/retry/exponential"
	"github.com/gostdlib/base/values/immutable"
)

const (
	// ctxIndex is the scratch slot holding ctx.Done(). This never moves.
	ctxIndex = 0
	// wakeIndex is the scratch slot holding the wake channel. This never moves.
	wakeIndex = 1
	// fixedCases is the number of consumer owned scratch slots that precede the user cases.
	fixedCases = 2
	// maxCases is the most user cases we allow. reflect.Select panics above 65536 total cases and TrySelect
	// appends one more case for its default.
	maxCases = 65536 - fixedCases - 1
)

// Errors returned by this package. All of them indicate API misuse that a retry cannot fix, so they are also
// wrapped with exponential.ErrPermanent.
var (
	// ErrNilChan indicates a nil channel was passed to AddRecv() or AddSend().
	ErrNilChan = fmt.Errorf("dynamic.Select does not support nil channels: %w", exponential.ErrPermanent)
	// ErrDupChan indicates the channel is already in use by another case.
	ErrDupChan = fmt.Errorf("cannot add the same channel to a dynamic.Select for two different cases: %w", exponential.ErrPermanent)
	// ErrTooManyCases indicates the Select is at the reflect.Select case limit.
	ErrTooManyCases = fmt.Errorf("dynamic.Select supports a maximum of %d cases: %w", maxCases, exponential.ErrPermanent)
	// ErrBadOption indicates an Option passed to New() was invalid.
	ErrBadOption = fmt.Errorf("invalid option: %w", exponential.ErrPermanent)
)

// Action is the action to take when a Select case occurs. Actions run on the goroutine that called Select(), after
// all internal locks are released, so an Action may safely call Add*() or Remove(). Actions run serially, so a slow
// Action delays every other case. If you need concurrency, hand the work to a pool from base/concurrency/worker.
type Action[T any] func(ctx context.Context, v T)

//go:generate go tool github.com/gostdlib/base/values/generators/stringer -type=ResultKind -linecomment

// ResultKind describes why a Select() returned and which fields of a Result are populated.
type ResultKind uint8

const (
	// RKUnknown indicates a Result that was never populated. This always indicates a bug.
	RKUnknown ResultKind = 0 // Unknown
	// Received indicates a value was received on Result.RecvCh and is stored in Result.Value.
	Received ResultKind = 1 // Received
	// Closed indicates Result.RecvCh was closed. The case was removed and no Action was run.
	Closed ResultKind = 2 // Closed
	// Sent indicates Result.Value was sent on Result.SendCh. The case was removed.
	Sent ResultKind = 3 // Sent
	// CtxDone indicates the Context passed to Select() was cancelled. RecvCh and SendCh are nil.
	CtxDone ResultKind = 4 // CtxDone
	// Defaulted indicates no case was ready. Only TrySelect() can return this.
	Defaulted ResultKind = 5 // Defaulted
)

// Result is the result of a Select.
type Result[T any] struct {
	// Kind says why Select() returned and which other fields are set. Always switch on this first.
	Kind ResultKind
	// RecvCh is the channel the case was on for Received and Closed. It is nil for every other Kind.
	RecvCh <-chan T
	// SendCh is the channel the case was on for Sent. It is nil for every other Kind.
	SendCh chan<- T
	// Value is the value received (Received) or sent (Sent). It is the zero value otherwise.
	Value T
}

// entryKind describes the channel operation an entry represents.
type entryKind uint8

const (
	// ekUnknown indicates an entry that was never populated. This always indicates a bug.
	ekUnknown entryKind = 0
	// ekRecv is a case that receives from a channel.
	ekRecv entryKind = 1
	// ekSend is a case that sends a value on a channel.
	ekSend entryKind = 2
)

// entry is a single dynamic case. An entry is immutable once it is published inside a snapshot. chVal and sendVal
// are precomputed at Add time so that rebuilding the scratch slice needs no reflect calls and no allocations.
type entry[T any] struct {
	id   uint64
	kind entryKind
	// key is the channel's identity, independent of its direction, and is the exists map key. It is safe as a
	// uintptr because chVal holds a live reference for as long as the entry is registered, so the channel can
	// never be collected out from under it, and the key is deleted on removal. It is never dereferenced.
	key     uintptr
	recvCh  <-chan T
	sendCh  chan<- T
	chVal   reflect.Value
	sendVal reflect.Value
	value   T
	action  Action[T]
}

// selectCase renders the entry as a reflect.SelectCase.
func (e entry[T]) selectCase() reflect.SelectCase {
	switch e.kind {
	case ekSend:
		return reflect.SelectCase{Dir: reflect.SelectSend, Chan: e.chVal, Send: e.sendVal}
	case ekRecv:
		return reflect.SelectCase{Dir: reflect.SelectRecv, Chan: e.chVal}
	}
	panic(fmt.Sprintf("bug: dynamic.Select entry has kind %d, which is not a valid entryKind", e.kind))
}

// snapshot is an immutable list of cases. Mutators publish a brand new snapshot under mu and the consumer reads
// the current one lock free. immutable.Slice makes "a published snapshot is never modified" a compiler enforced
// invariant rather than a comment: the consumer has no way to reslice it or assign to an element.
//
// This is an alias rather than a defined type on purpose. A defined type would not inherit Get/Len/All from
// immutable.Slice, which is the whole point of using it.
type snapshot[T any] = immutable.Slice[entry[T]]

// opts are the options for New().
type opts struct {
	// prealloc is the number of cases to preallocate room for in the case list, the channel index and the
	// consumer's scratch slice. Defaults to 0, which preallocates nothing.
	prealloc int
}

// Option is an option for New(). Options are applied in order, so a later option overrides an earlier one. All
// options are optional and a zero value asks for the default.
type Option func(opts) (opts, error)

// WithPreallocate preallocates room for n cases. Every mutation copies the case list, so a Select that will hold
// many channels avoids repeated slice growth by setting this. Defaults to 0.
func WithPreallocate(n int) Option {
	return func(o opts) (opts, error) {
		if n < 0 {
			return o, fmt.Errorf("%w: WithPreallocate cannot be < 0, was %d", ErrBadOption, n)
		}
		o.prealloc = n
		return o, nil
	}
}

// Select provides a dynamically configurable select statement where cases can be added or removed while a Select()
// is blocked. Exactly one goroutine may call Select(), TrySelect() or All(); Add*() and Remove() are safe from any
// goroutine. Use New() to create one, the zero value is not usable.
type Select[T any] struct {
	// mu guards the mutator side of the copy on write. The consumer only takes it to auto remove a finished case.
	mu     sync.Mutex
	exists map[uintptr]uint64
	nextID uint64

	// snap holds the current immutable snapshot. Readers load it without taking mu.
	snap atomic.Pointer[snapshot[T]]

	// wake is signalled by every mutator so that a blocked reflect.Select returns immediately. It is buffered by
	// 1 and the buffer means "something changed since you last drained", so a dropped send is never a lost
	// wakeup. See the proof on signal().
	wake    chan struct{}
	wakeVal reflect.Value

	// pending records whether a wake token is outstanding. The decision to skip a send must be a
	// sequentially consistent atomic RMW, not the runtime's racy channel-full check. See signal().
	pending atomic.Bool

	// The following are consumer owned and only touched by the single goroutine inside Select()/TrySelect().
	scratch  []reflect.SelectCase
	lastSnap *snapshot[T]
	lastDone <-chan struct{}

	// inSelect enforces the single consumer contract. Without it, two Selects would corrupt scratch.
	inSelect atomic.Bool
}

// New creates a new Select.
func New[T any](options ...Option) (*Select[T], error) {
	o := opts{}
	for _, option := range options {
		var err error
		o, err = option(o)
		if err != nil {
			return nil, fmt.Errorf("dynamic.New: %w", err)
		}
	}

	s := &Select[T]{
		exists: make(map[uintptr]uint64, o.prealloc),
		wake:   make(chan struct{}, 1),
	}
	s.wakeVal = reflect.ValueOf((<-chan struct{})(s.wake))

	empty := immutable.NewSlice[entry[T]](nil)
	s.snap.Store(&empty)

	s.scratch = make([]reflect.SelectCase, fixedCases, fixedCases+o.prealloc)
	// Setting Dir here is required, not tidiness. reflect.SelectDir counts from one (SelectSend is 1), so a zero
	// SelectCase has Dir == 0, which reflect.Select rejects outright with "invalid Dir". A valid Dir paired with
	// an invalid Chan is the shape we want: reflect.Select skips it, so the slot is simply never ready until
	// Select() supplies a real ctx.Done().
	s.scratch[ctxIndex] = reflect.SelectCase{Dir: reflect.SelectRecv}
	s.scratch[wakeIndex] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: s.wakeVal}
	return s, nil
}

// store publishes next as the current snapshot. This must be called with mu held. It does NOT signal, because the
// caller decides: the consumer removing its own finished case does not need to wake itself, and a wasted
// reflect.Select call costs an allocation per receive case.
func (s *Select[T]) store(next snapshot[T]) {
	s.snap.Store(&next)
}

// signal tells a blocked reflect.Select to return so that it picks up the newest snapshot. This must always happen
// AFTER store(), never before, and it must be called with mu held.
//
// The obvious implementation, a non-blocking `select { case wake <- struct{}{}: default: }`, is NOT safe here. A
// non-blocking send compiles to chansend(block=false), whose first action is a lock-free fast path
// (`if !block && c.closed == 0 && full(c) { return false }`) that never acquires the channel lock; the runtime's
// own comment there says "nothing here guarantees forward progress". So a dropped send establishes no
// happens-before edge with the consumer's receive, and a mutator could publish a snapshot the consumer never
// observes, leaving Select parked on a stale case set until the next mutation.
//
// Instead the drop decision is a sequentially consistent Swap. Because SC atomics are totally ordered,
// store() < Swap(true) < Store(false) < Load() holds whenever a signal is skipped, so a skipped signal provably
// implies the consumer reloads the snapshot afterwards.
//
// The send cannot block: pending == false implies the buffer is empty, and signal() is only ever called with mu
// held, so no other mutator can fill it in between.
func (s *Select[T]) signal() {
	if !s.pending.Swap(true) {
		s.wake <- struct{}{}
	}
}

// add copies the current snapshot, appends e and publishes the copy.
func (s *Select[T]) add(e entry[T]) error {
	return s.addAll([]entry[T]{e})
}

// addAll copies the current snapshot once, appends every entry in entries and publishes the copy. It is all or
// nothing: if any entry is already registered, or the batch would exceed maxCases, nothing is added. Adding n cases
// this way costs one snapshot copy rather than the n copies that n calls to add() would cost.
func (s *Select[T]) addAll(entries []entry[T]) error {
	if len(entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate the whole batch before mutating anything, so a rejected batch leaves the Select untouched.
	for _, e := range entries {
		if _, ok := s.exists[e.key]; ok {
			return ErrDupChan
		}
	}

	cur := *s.snap.Load()
	if cur.Len()+len(entries) > maxCases {
		return ErrTooManyCases
	}

	// Exact capacity, because the slice behind a published snapshot must never be appended into.
	raw := make([]entry[T], 0, cur.Len()+len(entries))
	for _, existing := range cur.All() {
		raw = append(raw, existing)
	}
	for _, e := range entries {
		s.nextID++
		e.id = s.nextID
		raw = append(raw, e)
		s.exists[e.key] = e.id
	}

	s.store(immutable.NewSlice(raw))
	s.signal()
	return nil
}

// AddRecv adds a case that receives from ch. When a value arrives, action is called with it and Select() returns a
// Received Result. If ch is closed, Select() returns a Closed Result, the case is removed and
// action is NOT called. action may be nil. ch cannot be nil and cannot already be in use by another case.
func (s *Select[T]) AddRecv(ch <-chan T, action Action[T]) error {
	if ch == nil {
		return ErrNilChan
	}
	chVal := reflect.ValueOf(ch)
	return s.add(entry[T]{kind: ekRecv, key: chVal.Pointer(), recvCh: ch, chVal: chVal, action: action})
}

// AddRecvAll adds a receive case for every channel in chs, all in a single snapshot update. It is equivalent to
// calling AddRecv once per channel with the same action, but costs one snapshot copy instead of len(chs), so
// registering n channels is O(n) rather than O(n^2). It is all or nothing: if any channel is nil, is already in use,
// or is repeated within chs, nothing is added and the error is returned. action may be nil.
func (s *Select[T]) AddRecvAll(action Action[T], chs ...<-chan T) error {
	if len(chs) == 0 {
		return nil
	}

	entries := make([]entry[T], 0, len(chs))
	seen := make(map[uintptr]bool, len(chs))
	for _, ch := range chs {
		if ch == nil {
			return ErrNilChan
		}
		chVal := reflect.ValueOf(ch)
		key := chVal.Pointer()
		if seen[key] {
			return ErrDupChan
		}
		seen[key] = true
		entries = append(entries, entry[T]{kind: ekRecv, key: key, recvCh: ch, chVal: chVal, action: action})
	}
	return s.addAll(entries)
}

// AddSend adds a case that sends value on ch. The case is one shot: once the send completes, action is called with
// value, the case is removed and Select() returns a Sent Result. action may be nil. ch cannot be nil and
// cannot already be in use by another case. Closing ch while a send case is pending panics inside Select(), exactly
// as it would in a hand written select statement.
func (s *Select[T]) AddSend(ch chan<- T, value T, action Action[T]) error {
	if ch == nil {
		return ErrNilChan
	}

	chVal := reflect.ValueOf(ch)
	e := entry[T]{
		kind:   ekSend,
		key:    chVal.Pointer(),
		sendCh: ch,
		chVal:  chVal,
		// This is deliberately not reflect.ValueOf(value). If T is an interface type holding nil, that yields
		// the invalid Value and reflect.Select panics with "SendDir case missing Send value". Going through a
		// pointer always yields a Value whose type is exactly T.
		sendVal: reflect.ValueOf(&value).Elem(),
		value:   value,
		action:  action,
	}
	return s.add(e)
}

// Remove removes ch from the Select and reports whether it was present. ch may be a bidirectional, receive only or
// send only channel of T, including a defined type whose underlying type is one of those; it is matched by channel
// identity, not by the static type it was added with. A nil ch reports false. Passing anything that is not a channel
// of T panics, because that is a programming error rather than a runtime condition.
//
// Remove is not a synchronization point, and it does not retract a select that is already running. A blocked
// Select() is parked on the previous case set, which still contains ch, so it can still receive from ch and run
// its Action after Remove has returned; whichever channel operation reaches the parked consumer first wins. The
// removal is guaranteed only from the point that in flight Select() returns and re-arms on the new case set.
//
// Suppressing such a delivery would mean discarding a value the runtime had already taken off the channel, which
// is worse than a late one. If you need a hard barrier, close ch instead and wait for a Closed Result, which is by
// definition the last event for that channel.
func (s *Select[T]) Remove(ch any) bool {
	if ch == nil {
		return false
	}

	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan || v.Type().Elem() != reflect.TypeFor[T]() {
		panic(fmt.Sprintf("Select.Remove: want a channel of %s, got %T", reflect.TypeFor[T](), ch))
	}
	if v.IsNil() {
		return false
	}
	return s.remove(v.Pointer())
}

// remove removes the case registered under key and reports whether it was present.
func (s *Select[T]) remove(key uintptr) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.exists[key]
	if !ok {
		return false
	}
	s.removeID(id)
	s.signal()
	return true
}

// removeID removes the entry with the given id and is a noop if it is already gone. It removes by id rather than by
// channel so that a channel which was removed and re-added concurrently does not lose its new case. This must be
// called with mu held and it does not signal.
func (s *Select[T]) removeID(id uint64) {
	cur := *s.snap.Load()

	raw := make([]entry[T], 0, max(cur.Len()-1, 0))
	var removed entry[T]
	found := false
	for _, e := range cur.All() {
		if e.id == id {
			removed = e
			found = true
			continue
		}
		raw = append(raw, e)
	}
	if !found {
		return
	}

	// Only drop the map key if it still points at the entry we removed.
	if s.exists[removed.key] == id {
		delete(s.exists, removed.key)
	}
	s.store(immutable.NewSlice(raw))
}

// Len returns the number of cases currently registered. This is safe to call from any goroutine.
func (s *Select[T]) Len() int {
	return s.snap.Load().Len()
}

// Select blocks until one of the cases can proceed and returns a Result. ctx.Done() is always an implicit case, so
// when ctx is cancelled Select returns a CtxDone Result. Cases may be added or removed by other
// goroutines while Select is blocked and take effect immediately. Only one goroutine may be inside Select(),
// TrySelect() or All() at a time and an Action must not call any of them.
func (s *Select[T]) Select(ctx context.Context) Result[T] {
	s.acquire()
	defer s.inSelect.Store(false)

	return s.selector(ctx, false)
}

// acquire takes the single consumer guard. It panics rather than corrupting the consumer owned scratch state,
// which is what two concurrent consumers would otherwise do silently.
func (s *Select[T]) acquire() {
	if !s.inSelect.CompareAndSwap(false, true) {
		panic("dynamic.Select: Select/TrySelect/All called concurrently, or re-entrantly from an Action")
	}
}

// TrySelect is Select() with a default case. If no case is immediately ready it returns a Defaulted Result
// instead of blocking.
func (s *Select[T]) TrySelect(ctx context.Context) Result[T] {
	s.acquire()
	defer s.inSelect.Store(false)

	return s.selector(ctx, true)
}

// selector is the control loop. Everything it touches is either consumer owned or an immutable snapshot pinned in
// a local, so it runs entirely lock free until it has to remove a finished case.
func (s *Select[T]) selector(ctx context.Context, nonBlocking bool) Result[T] {
	// ctx cannot change during a call, so resolve Done() once.
	done := ctx.Done()

	for {
		// reflect.Select picks uniformly at random among ready cases, so a cancelled Context would only win
		// against a ready channel about half the time. Check it first so cancellation actually wins, which is
		// what Select's doc promises and what a caller looping on CtxDone depends on.
		select {
		case <-done:
			return Result[T]{Kind: CtxDone}
		default:
		}

		// Load exactly once per iteration. Reading s.snap again below would let a concurrent mutation pair a
		// "chosen" computed against one snapshot with an entry read from another. This local is the only truth.
		snap := s.snap.Load()
		s.sync(snap, done)

		cases := s.scratch
		if nonBlocking {
			// Into a local, never back into s.scratch, or every later blocking Select would become a spin.
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectDefault})
		}

		// recvVal, not recv: a local named recv would shadow the entryKind constant of that name.
		chosen, recvVal, recvOK := reflect.Select(cases)

		switch {
		case chosen == ctxIndex:
			return Result[T]{Kind: CtxDone}
		case chosen == wakeIndex:
			// A mutator changed the case set. Reload the snapshot and rebuild. Clearing pending after the
			// receive is what lets the next mutator send again; never drain the channel any other way.
			s.pending.Store(false)
			continue
		case chosen == len(s.scratch):
			// Only reachable when nonBlocking is true.
			return Result[T]{Kind: Defaulted}
		}

		return s.finish(ctx, snap.Get(chosen-fixedCases), recvVal, recvOK)
	}
}

// sync makes the consumer owned scratch slice agree with snap and done. In the steady state both checks fail and
// this costs nothing: no lock, no allocation and no reflect call.
func (s *Select[T]) sync(snap *snapshot[T], done <-chan struct{}) {
	if s.lastDone != done {
		s.lastDone = done
		// A nil done yields a valid nil chan Value, which is never ready. That is correct for a Context that
		// cannot be cancelled: the select still returns as soon as a mutator signals wake.
		s.scratch[ctxIndex] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(done)}
	}

	// Snapshots are immutable and every mutation allocates a new one, so pointer equality means "unchanged". The
	// old snapshot cannot be freed while lastSnap references it, so its address cannot be recycled underneath us.
	if s.lastSnap == snap {
		return
	}
	s.lastSnap = snap

	s.scratch = s.scratch[:fixedCases]
	for _, e := range snap.All() {
		s.scratch = append(s.scratch, e.selectCase())
	}
	// Drop references to removed channels still sitting in the unused tail of the backing array.
	clear(s.scratch[len(s.scratch):cap(s.scratch)])
}

// finish does the bookkeeping for a chosen user case and then runs its Action. The lock is always released before
// the Action runs, so an Action may call Add*() or Remove() without deadlocking.
func (s *Select[T]) finish(ctx context.Context, e entry[T], recvVal reflect.Value, recvOK bool) Result[T] {
	switch e.kind {
	case ekRecv:
	case ekSend:
		// A send case is one shot: the value is on the channel now, so drop the case. recvVal and recvOK are
		// meaningless for a send, where recvVal is the invalid Value, and must not be touched.
		s.mu.Lock()
		s.removeID(e.id)
		s.mu.Unlock()

		if e.action != nil {
			e.action(ctx, e.value)
		}
		return Result[T]{Kind: Sent, SendCh: e.sendCh, Value: e.value}
	default:
		panic(fmt.Sprintf("bug: dynamic.Select entry has kind %d, which is not a valid entryKind", e.kind))
	}

	if !recvOK {
		// The channel is closed and can never produce another value, so remove it. The Action is deliberately
		// not called, because the zero value we just got was not sent by anybody.
		s.mu.Lock()
		s.removeID(e.id)
		s.mu.Unlock()

		var zero T
		return Result[T]{Kind: Closed, RecvCh: e.recvCh, Value: zero}
	}

	// The comma ok form matters: if T is an interface type and the value is nil, a plain assertion panics.
	v, _ := recvVal.Interface().(T)
	if e.action != nil {
		e.action(ctx, v)
	}
	return Result[T]{Kind: Received, RecvCh: e.recvCh, Value: v}
}

// All returns an iterator that calls Select() and returns the Results. It stops when the loop body breaks or when ctx is done,
// yielding a final CtxDone Result first. Only one goroutine may iterate at a time.
func (s *Select[T]) All(ctx context.Context) iter.Seq[Result[T]] {
	return func(yield func(Result[T]) bool) {
		// The guard is held for the whole iteration, including across yield. Taking it per Select() call would
		// leave it free at the yield point, so two goroutines ranging All() would interleave instead of
		// panicking and would silently share the consumer owned scratch state.
		s.acquire()
		defer s.inSelect.Store(false)

		for {
			// No ctx pre-check is needed here: selector() checks Done() before every reflect.Select, so a
			// cancelled Context always surfaces as CtxDone rather than losing a coin flip to a ready channel.
			r := s.selector(ctx, false)
			if !yield(r) {
				return
			}
			if r.Kind == CtxDone {
				return
			}
		}
	}
}
