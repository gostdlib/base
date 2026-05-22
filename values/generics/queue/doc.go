// Package queue provides a generic, thread-safe ordered list + queue that is integrated with OTEL
// and supplies multiple backing options.
// It supports blocking and non-blocking operations, and both on-disk and in-memory
// queues support a backup interface for recovery after a crash or other failure.
//
// The backing data structure is chosen by the caller and passed to New. Construct one
// with NewFIFO, NewBTreeFIFO, NewPriority, NewBTreePriority, NewBboltFIFO or
// NewBboltPriority:
//
//   - NewFIFO: in-memory slice FIFO, best for queues under ~10K items.
//   - NewBTreeFIFO: in-memory B-Tree FIFO for large/unbounded queues. Without
//     WithIndex it uses a positional tree with cheap push/pop and O(n) Exists/Del;
//     with WithIndex it uses a keyed tree plus a hash index for O(1) Exists and
//     O(log n) Del.
//   - NewPriority: in-memory heap priority queue; items pop in Item.Less order.
//   - NewBTreePriority: in-memory B-Tree priority queue, preferred for large or
//     unbounded priority queues (it avoids the heap's reheapify cost on Del) and
//     accepts WithIndex.
//   - NewBboltFIFO / NewBboltPriority: on-disk queues backed by go.etcd.io/bbolt; the
//     database is the source of truth on reopen.
//
// WithIndex enables an in-memory hash index for O(1) Exists / O(log n) Del; it is
// honored by the keyed B-Tree backings and the BoltDB backings. WithBTreeWidth tunes
// the keyed B-Tree backings. Priority backings order items by their Less method;
// FIFO backings order by insertion. Items pushed onto a priority queue must report
// Priority() > 0; items pushed onto a FIFO queue must report Priority() == 0.
//
// Built-in Item implementations (Number, String, Bytes, Value) cover the common cases;
// see the package examples for end-to-end usage of the FIFO, priority and peek paths.
package queue
