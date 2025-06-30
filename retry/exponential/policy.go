package exponential

import (
	"github.com/Azure/retry/exponential"
)

// Policy is the configuration for the backoff policy. Generally speaking you should use the
// default policy, but you can create your own if you want to customize it. But think long and
// hard about it before you do, as the default policy is a good mechanism for avoiding thundering
// herd problems, which are always remote calls. If not doing remote calls, you should question the use
// of this package. Note that a Policy is ignored if the service returns a delay in the error message.
type Policy = exponential.Policy

// TimeTableEntry is an entry in the time table.
type TimeTableEntry = exponential.TimeTableEntry

// TimeTable is a table of intervals describing the wait time between retries. This is useful for
// both testing and understanding what a policy will do.
type TimeTable = exponential.TimeTable

// FastRetryPolicy returns a retry plan that is fast at first and then slows down. This is the default policy.
//
// progression will be:
// 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s, 51.2s, 60s
// Not counting a randomization factor which will be +/- up to 50% of the interval.
func FastRetryPolicy() Policy {
	return exponential.FastRetryPolicy()
}

// SecondsRetryPolicy returns a retry plan that moves in 1 second intervals up to 60 seconds.
//
// progression will be:
// 1s, 2s, 4s, 8s, 16s, 32s, 60s
// Not counting a randomization factor which will be +/- up to 50% of the interval.
func SecondsRetryPolicy() Policy {
	return exponential.SecondsRetryPolicy()
}

// ThirtySecondsRetryPolicy returns a retry plan that moves in 30 second intervals up to 5 minutes.
//
// progression will be:
// 30s, 33s, 36s, 40s, 44s, 48s, 53s, 58s, 64s, 70s, 77s, 85s, 94s, 103s, 113s, 124s, 136s, 150s,
// 165s, 181s, 199s, 219s, 241s, 265s, 292s, 300s
// Not counting a randomization factor which will be +/- up to 20% of the interval.
func ThirtySecondsRetryPolicy() Policy {
	return exponential.ThirtySecondsRetryPolicy()
}
