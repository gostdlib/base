package queue

import (
	"bytes"
	"fmt"
	"hash/maphash"
	"math"
	"reflect"

	json "github.com/go-json-experiment/json"
	"golang.org/x/exp/constraints"
)

// validate the constraints.
var (
	_ Item[Number[uint8]]   = Number[uint8]{}
	_ Item[Number[int64]]   = Number[int64]{}
	_ Item[Number[float64]] = Number[float64]{}
	_ Item[String]          = String{}
	_ Item[Bytes]           = Bytes{}
	_ Item[Value[int]]      = Value[int]{}
)

// hashSeed makes maphash output stable for the lifetime of the process. Hash only needs
// to be self-consistent within one run: the WithIndex maps are in-memory and rebuilt at
// startup, so a per-process seed is sufficient.
var hashSeed = maphash.MakeSeed()

// NumberConstraint is the set of types Number can wrap.
type NumberConstraint interface {
	constraints.Float | constraints.Integer
}

// Number is a generic wrapper around a numeric type that implements the Item constraint.
type Number[T NumberConstraint] struct {
	// V is the underlying value.
	V T
	// P is the priority of the item. Set P > 0 to use this in a priority queue; leave
	// P == 0 for a FIFO queue. A priority queue rejects items with P == 0
	// (ErrPriorityRequired); a FIFO queue rejects items with P > 0 (ErrPriorityNotAllowed).
	P uint64
}

// Less implements Item.Less by comparing the priorities.
func (u Number[T]) Less(other Number[T]) bool {
	return u.P < other.P
}

// Equal implements Item.Equal by comparing the underlying values for equality.
func (u Number[T]) Equal(other Number[T]) bool {
	return u.V == other.V
}

// Priority implements Item.Priority by returning the priority P.
func (u Number[T]) Priority() uint64 {
	return u.P
}

// Hash implements Item.Hash with a value-derived key consistent with Equal. For integers
// the encoding is injective; for floats the value is canonicalized (adding +0.0 collapses
// -0.0 to +0.0) so Equal values (-0.0 == +0.0) hash equally; NaN never compares Equal.
func (u Number[T]) Hash() uint64 {
	// reflect.Kind reports the underlying kind, so this is correct for defined
	// numeric types (e.g. type ID int64), which NumberConstraint admits via ~.
	rv := reflect.ValueOf(u.V)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(rv.Int()) ^ (1 << 63)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(rv.Float() + 0.0)
	default:
		panic(fmt.Sprintf("queue: Number.Hash: unsupported kind %s", rv.Kind()))
	}
}

// String is a wrapper around a string that implements the Item constraint.
type String struct {
	// V is the underlying value.
	V string
	// P is the priority of the item. Set P > 0 to use this in a priority queue; leave
	// P == 0 for a FIFO queue. A priority queue rejects items with P == 0
	// (ErrPriorityRequired); a FIFO queue rejects items with P > 0 (ErrPriorityNotAllowed).
	P uint64
}

// Less implements Item.Less by comparing the priorities.
func (u String) Less(other String) bool {
	return u.P < other.P
}

// Equal implements Item.Equal by comparing the underlying values for equality.
func (u String) Equal(other String) bool {
	return u.V == other.V
}

// Priority implements Item.Priority by returning the priority P.
func (u String) Priority() uint64 {
	return u.P
}

// Hash implements Item.Hash with a value hash consistent with ==.
func (u String) Hash() uint64 {
	return maphash.String(hashSeed, u.V)
}

// Bytes is a wrapper around a []byte that implements the Item constraint.
type Bytes struct {
	// V is the underlying value.
	V []byte
	// P is the priority of the item. Set P > 0 to use this in a priority queue; leave
	// P == 0 for a FIFO queue. A priority queue rejects items with P == 0
	// (ErrPriorityRequired); a FIFO queue rejects items with P > 0 (ErrPriorityNotAllowed).
	P uint64
}

// Less implements Item.Less by comparing the priorities.
func (u Bytes) Less(other Bytes) bool {
	return u.P < other.P
}

// Equal implements Item.Equal by comparing the underlying values for equality.
func (u Bytes) Equal(other Bytes) bool {
	return bytes.Equal(u.V, other.V)
}

// Priority implements Item.Priority by returning the priority P.
func (u Bytes) Priority() uint64 {
	return u.P
}

// Hash implements Item.Hash with a value hash consistent with bytes.Equal.
func (u Bytes) Hash() uint64 {
	return maphash.Bytes(hashSeed, u.V)
}

// Value adapts an arbitrary type to the Item constraint using caller-supplied equality
// and hash functions. Equaler and Hasher must be non-nil and consistent: if Equaler(a, b)
// then Hasher(a) == Hasher(b).
//
// For the in-memory backings nothing else is required. For the on-disk bbolt backings a
// codec must be supplied to the constructor with WithCodec, because Value's function
// fields cannot be serialized: NewBboltFIFO/NewBboltPriority return ErrCodecRequired if a
// Value queue is built without WithCodec. The package provides JSONEncode/JSONDecode for
// any JSON-serializable type; for a Value the decode closure should also re-attach
// Equaler and Hasher (they are not persisted) if Del/Exists/WithIndex are used.
type Value[T any] struct {
	// V is the underlying value.
	V T
	// P is the priority of the item. Set P > 0 to use this in a priority queue; leave
	// P == 0 for a FIFO queue. A priority queue rejects items with P == 0
	// (ErrPriorityRequired); a FIFO queue rejects items with P > 0 (ErrPriorityNotAllowed).
	P uint64
	// Equaler reports whether two values are equal. Must be non-nil and consistent with Hasher.
	Equaler func(T, T) bool
	// Hasher returns a value-derived bucket key consistent with Equaler: if
	// Equaler(a, b) then Hasher(a) == Hasher(b). Must be non-nil.
	Hasher func(T) uint64
}

// requiresDiskCodec marks Value as needing a WithCodec on the on-disk backings (its
// function fields are not serializable by the default JSON codec). It is detected by
// NewBboltFIFO/NewBboltPriority to reject a codec-less Value queue with ErrCodecRequired.
func (Value[T]) requiresDiskCodec() {}

// Less implements Item.Less by comparing the priorities.
func (u Value[T]) Less(other Value[T]) bool {
	return u.P < other.P
}

// Equal implements Item.Equal using the caller-supplied Equaler.
func (u Value[T]) Equal(other Value[T]) bool {
	if u.Equaler == nil {
		panic("queue: Value.Equal: Equaler is nil")
	}
	return u.Equaler(u.V, other.V)
}

// Priority implements Item.Priority by returning the priority P.
func (u Value[T]) Priority() uint64 {
	return u.P
}

// Hash implements Item.Hash using the caller-supplied Hasher.
func (u Value[T]) Hash() uint64 {
	if u.Hasher == nil {
		panic("queue: Value.Hash: Hasher is nil")
	}
	return u.Hasher(u.V)
}

// JSONEncode writes v as JSON into dst using github.com/go-json-experiment/json. Pass it
// (or a closure wrapping it) to WithCodec as the encoder for an on-disk queue.
func JSONEncode[T any](dst *bytes.Buffer, v T) error {
	return json.MarshalWrite(dst, v)
}

// JSONDecode reads JSON written by JSONEncode into dst (reused across items) using
// github.com/go-json-experiment/json. Pass it to WithCodec as the decoder.
func JSONDecode[T any](src []byte, dst *T) error {
	return json.Unmarshal(src, dst)
}
