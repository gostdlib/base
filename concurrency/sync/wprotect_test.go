package sync

import (
	"context"
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
)

type CopyValue struct {
	String string
	Slice  []int
}

func (c *CopyValue) Copy() *CopyValue {
	ns := make([]int, len(c.Slice))
	copy(ns, c.Slice)

	return &CopyValue{
		String: c.String, // strings are immutable, so this is safe
		Slice:  ns,
	}
}

func TestWriteProtect(t *testing.T) {
	time.Sleep(1 * time.Second)
	ctx := context.Background()
	wp := WProtect[CopyValue, *CopyValue] {}
	want := &CopyValue{String: "hello", Slice: []int{1, 2, 3}}

	wp.Set(want)

	if diff := pretty.Compare(want, wp.Get()); diff != "" {
		t.Errorf("TestWriteProtect: -want/+got:\n%s", diff)
	}

	changed := want.Copy()
	changed.String = "goodbye"
	changed.Slice[0] = 0

	g := Group{}

	for i := 0; i < 100000; i++ {
		g.Go(
			ctx,
			func(ctx context.Context) error {
				got := wp.Get()
				if diff := pretty.Compare(want, got); diff != "" {
					if diff := pretty.Compare(changed, got); diff != "" {
						t.Errorf("TestWriteProtect: -want/+got:\n%s", diff)
					}
				}
				return nil
			},
		)
	}

	time.Sleep(5 * time.Millisecond)
	if err := wp.Set(changed); err != nil {
		panic(err)
	}

	g.Wait(ctx)

	if diff := pretty.Compare(changed, wp.Get()); diff != "" {
		t.Errorf("TestWriteProtect: -want/+got:\n%s", diff)
	}
}
