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
