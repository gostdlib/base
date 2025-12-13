package worker

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

func TestSliceSeq2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		slice []int
		want  []int
	}{
		{
			name:  "Success: empty slice",
			slice: []int{},
			want:  []int{},
		},
		{
			name:  "Success: single element",
			slice: []int{42},
			want:  []int{42},
		},
		{
			name:  "Success: multiple elements",
			slice: []int{1, 2, 3, 4, 5},
			want:  []int{1, 2, 3, 4, 5},
		},
	}

	for _, test := range tests {
		seq := SliceSeq2(test.slice)
		got := make([]int, len(test.slice))
		for i, v := range seq {
			got[i] = v
		}
		if diff := pretty.Compare(test.want, got); diff != "" {
			t.Errorf("TestSliceSeq2(%s): -want +got:\n%s", test.name, diff)
		}
	}
}

func TestSliceSeq2EarlyBreak(t *testing.T) {
	t.Parallel()

	slice := []int{1, 2, 3, 4, 5}
	seq := SliceSeq2(slice)

	var got []int
	for _, v := range seq {
		got = append(got, v)
		if v == 3 {
			break
		}
	}

	want := []int{1, 2, 3}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestSliceSeq2EarlyBreak: -want +got:\n%s", diff)
	}
}

func TestMapSeq2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    map[string]int
	}{
		{
			name: "Success: empty map",
			m:    map[string]int{},
		},
		{
			name: "Success: single element",
			m:    map[string]int{"a": 1},
		},
		{
			name: "Success: multiple elements",
			m:    map[string]int{"a": 1, "b": 2, "c": 3},
		},
	}

	for _, test := range tests {
		seq := MapSeq2(test.m)
		got := make(map[string]int)
		for k, v := range seq {
			got[k] = v
		}
		if diff := pretty.Compare(test.m, got); diff != "" {
			t.Errorf("TestMapSeq2(%s): -want +got:\n%s", test.name, diff)
		}
	}
}

func TestMapSeq2EarlyBreak(t *testing.T) {
	t.Parallel()

	m := map[string]int{"a": 1, "b": 2, "c": 3}
	seq := MapSeq2(m)

	count := 0
	for range seq {
		count++
		if count == 2 {
			break
		}
	}

	if count != 2 {
		t.Errorf("TestMapSeq2EarlyBreak: got count %d, want 2", count)
	}
}

func TestChanSeq2(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []int
	}{
		{
			name:   "Success: empty channel",
			values: []int{},
		},
		{
			name:   "Success: single element",
			values: []int{42},
		},
		{
			name:   "Success: multiple elements",
			values: []int{1, 2, 3, 4, 5},
		},
	}

	for _, test := range tests {
		ch := make(chan int, len(test.values))
		for _, v := range test.values {
			ch <- v
		}
		close(ch)

		seq := ChanSeq2(ch)
		var got []int
		for _, v := range seq {
			got = append(got, v)
		}

		if diff := pretty.Compare(test.values, got); diff != "" {
			t.Errorf("TestChanSeq2(%s): -want +got:\n%s", test.name, diff)
		}
	}
}

func TestChanSeq2EarlyBreak(t *testing.T) {
	t.Parallel()

	ch := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)

	seq := ChanSeq2(ch)
	var got []int
	for _, v := range seq {
		got = append(got, v)
		if v == 3 {
			break
		}
	}

	want := []int{1, 2, 3}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestChanSeq2EarlyBreak: -want +got:\n%s", diff)
	}
}

func TestChanSeq2ReceiveOnly(t *testing.T) {
	t.Parallel()

	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)

	var recvCh <-chan int = ch

	seq := ChanSeq2(recvCh)
	var got []int
	for _, v := range seq {
		got = append(got, v)
	}

	want := []int{1, 2, 3}
	if diff := pretty.Compare(want, got); diff != "" {
		t.Errorf("TestChanSeq2ReceiveOnly: -want +got:\n%s", diff)
	}
}

func TestWait(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testWaitPool")
	if err != nil {
		t.Fatalf("TestWait: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	tests := []struct {
		name    string
		slice   []int
		f       Func[int, int]
		wantErr bool
	}{
		{
			name:  "Success: empty slice",
			slice: []int{},
			f: func(ctx context.Context, k int, v int) error {
				return nil
			},
			wantErr: false,
		},
		{
			name:  "Success: process all elements",
			slice: []int{1, 2, 3, 4, 5},
			f: func(ctx context.Context, k int, v int) error {
				return nil
			},
			wantErr: false,
		},
		{
			name:  "Error: function returns error",
			slice: []int{1, 2, 3},
			f: func(ctx context.Context, k int, v int) error {
				if v == 2 {
					return errors.New("test error")
				}
				return nil
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		results := make([]atomic.Bool, len(test.slice))
		f := func(ctx context.Context, k int, v int) error {
			results[k].Store(true)
			return test.f(ctx, k, v)
		}

		seq := SliceSeq2(test.slice)
		err := Wait[int, int, any](ctx, pool, seq, f)

		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestWait(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestWait(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		for i := range results {
			if !results[i].Load() {
				t.Errorf("TestWait(%s): element %d was not processed", test.name, i)
			}
		}
	}
}

func TestWaitWithCancelOnError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testWaitCancelPool")
	if err != nil {
		t.Fatalf("TestWaitWithCancelOnError: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	slice := []int{1, 2, 3, 4, 5}
	seq := SliceSeq2(slice)

	f := func(ctx context.Context, k int, v int) error {
		if v == 2 {
			return errors.New("test error")
		}
		return nil
	}

	err = Wait[int, int, any](ctx, pool, seq, f, WithCancelOnError())
	if err == nil {
		t.Errorf("TestWaitWithCancelOnError: got err == nil, want err != nil")
	}
}

func TestSeq(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testSeqPool")
	if err != nil {
		t.Fatalf("TestSeq: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	slice := []int{1, 2, 3, 4, 5}
	results := make([]atomic.Bool, len(slice))

	seq := SliceSeq2(slice)
	f := func(ctx context.Context, k int, v int) error {
		results[k].Store(true)
		return nil
	}

	Seq(ctx, pool, seq, f)
	pool.Wait()

	for i := range results {
		if !results[i].Load() {
			t.Errorf("TestSeq: element %d was not processed", i)
		}
	}
}

func TestSeqWithCancelOnError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testSeqCancelPool")
	if err != nil {
		t.Fatalf("TestSeqWithCancelOnError: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	slice := []int{0}
	seq := SliceSeq2(slice)

	f := func(ctx context.Context, k int, v int) error {
		return errors.New("test error")
	}

	Seq(ctx, pool, seq, f, WithCancelOnError())
	pool.Wait()
}

func TestSeqEmptySlice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testSeqEmptyPool")
	if err != nil {
		t.Fatalf("TestSeqEmptySlice: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	seq := SliceSeq2([]int{})
	callCount := atomic.Int64{}

	f := func(ctx context.Context, k int, v int) error {
		callCount.Add(1)
		return nil
	}

	Seq(ctx, pool, seq, f)
	pool.Wait()

	if callCount.Load() != 0 {
		t.Errorf("TestSeqEmptySlice: got %d calls, want 0", callCount.Load())
	}
}

func TestWaitWithMap(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testWaitMapPool")
	if err != nil {
		t.Fatalf("TestWaitWithMap: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	m := map[string]int{"a": 1, "b": 2, "c": 3}
	seq := MapSeq2(m)

	results := make(map[string]int)
	var mu atomic.Pointer[map[string]int]
	mu.Store(&results)

	f := func(ctx context.Context, k string, v int) error {
		return nil
	}

	err = Wait[string, int, any](ctx, pool, seq, f)
	if err != nil {
		t.Errorf("TestWaitWithMap: got err == %s, want err == nil", err)
	}
}

func TestSeqWithChan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool, err := New(ctx, "testSeqChanPool")
	if err != nil {
		t.Fatalf("TestSeqWithChan: failed to create pool: %v", err)
	}
	defer pool.Close(ctx)

	ch := make(chan int, 5)
	for i := 1; i <= 5; i++ {
		ch <- i
	}
	close(ch)

	seq := ChanSeq2(ch)
	results := make([]atomic.Bool, 6)

	f := func(ctx context.Context, k int, v int) error {
		results[v].Store(true)
		return nil
	}

	Seq(ctx, pool, seq, f)
	pool.Wait()

	for i := 1; i <= 5; i++ {
		if !results[i].Load() {
			t.Errorf("TestSeqWithChan: element %d was not processed", i)
		}
	}
}

func TestWithGroupOptions(t *testing.T) {
	t.Parallel()

	o := opts{}
	o = WithGroupOptions()(o)

	if o.cancelOnErr {
		t.Error("TestWithGroupOptions: cancelOnErr should be false")
	}
}

func TestWithCancelOnError(t *testing.T) {
	t.Parallel()

	o := opts{}
	o = WithCancelOnError()(o)

	if !o.cancelOnErr {
		t.Error("TestWithCancelOnError: cancelOnErr should be true")
	}
}

func TestSliceSeq2Indices(t *testing.T) {
	t.Parallel()

	slice := []string{"a", "b", "c"}
	seq := SliceSeq2(slice)

	var indices []int
	var values []string
	for i, v := range seq {
		indices = append(indices, i)
		values = append(values, v)
	}

	wantIndices := []int{0, 1, 2}
	wantValues := []string{"a", "b", "c"}

	if !slices.Equal(indices, wantIndices) {
		t.Errorf("TestSliceSeq2Indices: got indices %v, want %v", indices, wantIndices)
	}
	if !slices.Equal(values, wantValues) {
		t.Errorf("TestSliceSeq2Indices: got values %v, want %v", values, wantValues)
	}
}

func TestChanSeq2Indices(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 3)
	ch <- "x"
	ch <- "y"
	ch <- "z"
	close(ch)

	seq := ChanSeq2(ch)

	var indices []int
	var values []string
	for i, v := range seq {
		indices = append(indices, i)
		values = append(values, v)
	}

	wantIndices := []int{0, 1, 2}
	wantValues := []string{"x", "y", "z"}

	if !slices.Equal(indices, wantIndices) {
		t.Errorf("TestChanSeq2Indices: got indices %v, want %v", indices, wantIndices)
	}
	if !slices.Equal(values, wantValues) {
		t.Errorf("TestChanSeq2Indices: got values %v, want %v", values, wantValues)
	}
}
