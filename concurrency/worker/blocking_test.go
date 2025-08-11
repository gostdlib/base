package worker

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSubPoolWithBlockingJobs tests with jobs that block to force queue backup
func TestSubPoolWithBlockingJobs(t *testing.T) {
	// Force very limited concurrency
	oldMaxProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(oldMaxProcs)
	
	ctx := context.Background()
	
	// Create parent pool with minimal workers
	p, err := New(ctx, "parentPool", WithSize(2), WithRunnerTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create parent pool: %v", err)
	}
	
	// Run multiple rounds to catch intermittent issues
	for round := 0; round < 10; round++ {
		sub := p.Sub(ctx, "subPool")
		
		submitted := atomic.Int64{}
		executed := atomic.Int64{}
		started := atomic.Int64{}
		
		const numJobs = 100
		
		t.Logf("Round %d: Submitting %d blocking jobs...", round, numJobs)
		
		// Submit jobs that block for a significant time
		for i := 0; i < numJobs; i++ {
			jobID := i
			err := sub.Submit(ctx, func() {
				started.Add(1)
				// Block for 1 second to ensure queue fills up
				time.Sleep(1 * time.Second)
				executed.Add(1)
				t.Logf("Job %d completed", jobID)
			})
			if err != nil {
				t.Errorf("Round %d: Failed to submit job %d: %v", round, i, err)
			} else {
				submitted.Add(1)
			}
		}
		
		t.Logf("Round %d: All %d jobs submitted, waiting for completion...", round, submitted.Load())
		
		// Wait for all jobs to complete
		sub.Wait()
		
		finalSubmitted := submitted.Load()
		finalExecuted := executed.Load()
		finalStarted := started.Load()
		
		t.Logf("Round %d: Submitted=%d, Started=%d, Executed=%d", 
			round, finalSubmitted, finalStarted, finalExecuted)
		
		if finalExecuted != finalSubmitted {
			t.Fatalf("Round %d: BUG DETECTED! Submitted %d jobs but only %d executed (lost %d jobs)",
				round, finalSubmitted, finalExecuted, finalSubmitted-finalExecuted)
		}
	}
}

// TestSubPoolMassiveBlockingLoad tests with very high job counts that block
func TestSubPoolMassiveBlockingLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping massive load test in short mode")
	}
	
	// Force minimal concurrency
	oldMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldMaxProcs)
	
	ctx := context.Background()
	
	// Create parent pool with only 1 worker
	p, err := New(ctx, "parentPool", WithSize(1), WithRunnerTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create parent pool: %v", err)
	}
	
	sub := p.Sub(ctx, "subPool")
	
	submitted := atomic.Int64{}
	executed := atomic.Int64{}
	maxConcurrent := atomic.Int64{}
	currentlyRunning := atomic.Int64{}
	
	const numJobs = 1000
	
	t.Logf("Submitting %d jobs that each block for 500ms...", numJobs)
	
	// Submit jobs that block, forcing many runners to be created
	for i := 0; i < numJobs; i++ {
		err := sub.Submit(ctx, func() {
			// Track max concurrent jobs
			running := currentlyRunning.Add(1)
			for {
				current := maxConcurrent.Load()
				if running > current {
					if maxConcurrent.CompareAndSwap(current, running) {
						break
					}
				} else {
					break
				}
			}
			
			// Block for 500ms
			time.Sleep(500 * time.Millisecond)
			
			currentlyRunning.Add(-1)
			executed.Add(1)
		})
		if err != nil {
			t.Errorf("Failed to submit job %d: %v", i, err)
		} else {
			submitted.Add(1)
		}
	}
	
	t.Logf("All jobs submitted. Waiting for completion...")
	t.Logf("Max concurrent before wait: %d", maxConcurrent.Load())
	
	// Wait for all jobs
	sub.Wait()
	
	finalSubmitted := submitted.Load()
	finalExecuted := executed.Load()
	
	t.Logf("Final: Submitted=%d, Executed=%d, MaxConcurrent=%d", 
		finalSubmitted, finalExecuted, maxConcurrent.Load())
	
	if finalExecuted != finalSubmitted {
		t.Fatalf("BUG! Submitted %d but executed %d (lost %d jobs)",
			finalSubmitted, finalExecuted, finalSubmitted-finalExecuted)
	}
}

// TestSubPoolConcurrentBlockingSubmissions tests many goroutines submitting blocking jobs
func TestSubPoolConcurrentBlockingSubmissions(t *testing.T) {
	oldMaxProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(oldMaxProcs)
	
	ctx := context.Background()
	
	p, err := New(ctx, "parentPool", WithSize(2), WithRunnerTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create parent pool: %v", err)
	}
	
	sub := p.Sub(ctx, "subPool")
	
	totalSubmitted := atomic.Int64{}
	totalExecuted := atomic.Int64{}
	
	const numSubmitters = 10
	const jobsPerSubmitter = 50
	
	t.Logf("Starting %d submitters, each submitting %d blocking jobs", numSubmitters, jobsPerSubmitter)
	
	var wg sync.WaitGroup
	
	for s := 0; s < numSubmitters; s++ {
		wg.Add(1)
		go func(submitterID int) {
			defer wg.Done()
			
			for j := 0; j < jobsPerSubmitter; j++ {
				err := sub.Submit(ctx, func() {
					// Block for 200ms
					time.Sleep(200 * time.Millisecond)
					totalExecuted.Add(1)
				})
				if err == nil {
					totalSubmitted.Add(1)
				} else {
					t.Errorf("Submitter %d job %d failed: %v", submitterID, j, err)
				}
			}
			t.Logf("Submitter %d completed submissions", submitterID)
		}(s)
	}
	
	// Wait for all submitters
	wg.Wait()
	
	t.Logf("All submissions complete. Total submitted: %d", totalSubmitted.Load())
	t.Logf("Waiting for execution to complete...")
	
	// Wait for all jobs to execute
	sub.Wait()
	
	finalSubmitted := totalSubmitted.Load()
	finalExecuted := totalExecuted.Load()
	
	t.Logf("Final: Submitted=%d, Executed=%d", finalSubmitted, finalExecuted)
	
	if finalExecuted != finalSubmitted {
		t.Fatalf("BUG! Submitted %d but executed %d (lost %d jobs)",
			finalSubmitted, finalExecuted, finalSubmitted-finalExecuted)
	}
}

// TestSubPoolRapidBlockingBursts tests rapid bursts of blocking jobs
func TestSubPoolRapidBlockingBursts(t *testing.T) {
	oldMaxProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(oldMaxProcs)
	
	ctx := context.Background()
	
	// Very small pool to maximize contention
	p, err := New(ctx, "parentPool", WithSize(1), WithRunnerTimeout(10*time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to create parent pool: %v", err)
	}
	
	const numBursts = 20
	const jobsPerBurst = 100
	
	for burst := 0; burst < numBursts; burst++ {
		sub := p.Sub(ctx, "subPool")
		
		submitted := atomic.Int64{}
		executed := atomic.Int64{}
		
		// Submit a burst of blocking jobs all at once
		for i := 0; i < jobsPerBurst; i++ {
			err := sub.Submit(ctx, func() {
				// Block to force queue backup
				time.Sleep(100 * time.Millisecond)
				executed.Add(1)
			})
			if err == nil {
				submitted.Add(1)
			}
		}
		
		// Wait for this burst to complete
		sub.Wait()
		
		if executed.Load() != submitted.Load() {
			t.Fatalf("Burst %d: BUG! Submitted %d but executed %d",
				burst, submitted.Load(), executed.Load())
		}
		
		if burst%5 == 0 {
			t.Logf("Completed burst %d/%d", burst, numBursts)
		}
	}
	
	t.Logf("All %d bursts completed successfully", numBursts)
}