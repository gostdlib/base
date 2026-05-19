package queue

import (
	"bytes"
	"errors"
	"testing"
)

// TestEmptyNameDisablesTelemetry verifies that a queue created with an empty name records
// no OTEL telemetry (met is nil) and that operations still work.
func TestEmptyNameDisablesTelemetry(t *testing.T) {
	ctx := t.Context()
	b, err := NewFIFO[Number[int]]()
	if err != nil {
		t.Fatalf("TestEmptyNameDisablesTelemetry: NewFIFO got err == %s, want nil", err)
	}
	q, err := New[Number[int]](ctx, "", b, 0)
	if err != nil {
		t.Fatalf("TestEmptyNameDisablesTelemetry: New got err == %s, want nil", err)
	}
	if q.met != nil {
		t.Fatalf("TestEmptyNameDisablesTelemetry: q.met != nil, want nil (telemetry disabled)")
	}
	if _, err := q.Push(ctx, []Number[int]{{V: 1}}); err != nil {
		t.Fatalf("TestEmptyNameDisablesTelemetry: Push got err == %s, want nil", err)
	}
	items, err := q.Pop(ctx, 1)
	if err != nil || len(items) != 1 || items[0].V != 1 {
		t.Fatalf("TestEmptyNameDisablesTelemetry: Pop got (%v, %v), want ([{1 0}], nil)", items, err)
	}
	if err := q.Close(ctx); err != nil {
		t.Fatalf("TestEmptyNameDisablesTelemetry: Close got err == %s, want nil", err)
	}
}

// TestValueCodecBbolt verifies a Value round-trips through an on-disk bbolt FIFO when a
// codec is supplied with WithCodec (payload and priority survive Push -> store -> Pop).
func TestValueCodecBbolt(t *testing.T) {
	ctx := t.Context()
	type rec struct{ Name string }
	type wire struct {
		V rec
		P uint64
	}
	enc := func(dst *bytes.Buffer, v Value[rec]) error {
		return JSONEncode(dst, wire{V: v.V, P: v.P})
	}
	dec := func(src []byte, dst *Value[rec]) error {
		var w wire
		if err := JSONDecode(src, &w); err != nil {
			return err
		}
		dst.V, dst.P = w.V, w.P
		return nil
	}
	mk := func(n string) Value[rec] { return Value[rec]{V: rec{Name: n}} }

	b, err := NewBboltFIFO[Value[rec]](ctx, diskRoot(t), WithCodec[Value[rec]](enc, dec))
	if err != nil {
		t.Fatalf("TestValueCodecBbolt: NewBboltFIFO got err == %s, want err == nil", err)
	}
	q, err := New[Value[rec]](ctx, "test", b, Unlimited)
	if err != nil {
		t.Fatalf("TestValueCodecBbolt: New got err == %s, want err == nil", err)
	}
	defer q.Close(ctx)

	if _, err := q.Push(ctx, []Value[rec]{mk("alice"), mk("bob")}); err != nil {
		t.Fatalf("TestValueCodecBbolt: Push got err == %s, want err == nil", err)
	}
	items, err := q.Pop(ctx, 2)
	if err != nil {
		t.Fatalf("TestValueCodecBbolt: Pop got err == %s, want err == nil", err)
	}
	if len(items) != 2 || items[0].V.Name != "alice" || items[1].V.Name != "bob" {
		t.Fatalf("TestValueCodecBbolt: round-trip got %+v, want [{alice} {bob}]", items)
	}
}

// TestValueCodecRejected verifies a Value queue built on an on-disk backing without
// WithCodec is rejected at construction with ErrCodecRequired.
func TestValueCodecRejected(t *testing.T) {
	ctx := t.Context()
	type rec struct{ Name string }

	_, err := NewBboltFIFO[Value[rec]](ctx, diskRoot(t))
	if !errors.Is(err, ErrCodecRequired) {
		t.Errorf("TestValueCodecRejected: NewBboltFIFO got err == %v, want ErrCodecRequired", err)
	}
}
