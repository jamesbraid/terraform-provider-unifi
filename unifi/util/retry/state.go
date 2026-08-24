// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package retry

import (
	"context"
	"log"
	"slices"
	"time"
)

var refreshGracePeriod = 30 * time.Second

// StateRefreshFunc refreshes the item being watched for a state change. It
// returns the current result and state, and any error encountered; a nil
// result means not found.
type StateRefreshFunc func() (result any, state string, err error)

// StateChangeConf is the configuration struct used for `WaitForState`.
type StateChangeConf struct {
	Delay          time.Duration    // Wait this time before starting checks
	Pending        []string         // States that are "allowed" and will continue trying
	Refresh        StateRefreshFunc // Refreshes the current state
	Target         []string         // Target state
	Timeout        time.Duration    // The amount of time to wait before timeout
	MinTimeout     time.Duration    // Smallest time to wait before refreshes
	PollInterval   time.Duration    // Override MinTimeout/backoff and only poll this often
	NotFoundChecks int64            // Number of times to allow not found (nil result from Refresh)

	ContinuousTargetOccurence int64 // Number of times the Target state has to occur continuously
}

// WaitForStateContext watches an object and waits for it to reach the Target
// state using the configured Refresh func.
//
// It returns an error immediately if Refresh errors or reports a state that
// is neither Target nor Pending, and a *TimeoutError if Timeout elapses first.
// Canceling ctx cancels the refresh loop.
func (conf *StateChangeConf) WaitForStateContext(ctx context.Context) (any, error) {
	log.Printf("[DEBUG] Waiting for state to become: %s", conf.Target)

	var targetOccurence, notfoundTick int64 = 0, 0

	if conf.NotFoundChecks == 0 {
		conf.NotFoundChecks = 20
	}

	if conf.ContinuousTargetOccurence == 0 {
		conf.ContinuousTargetOccurence = 1
	}

	type Result struct {
		Result any
		State  string
		Error  error
		Done   bool
	}

	resCh := make(chan Result, 1)
	cancelCh := make(chan struct{})

	result := Result{}

	go func() {
		defer close(resCh)

		select {
		case <-time.After(conf.Delay):
		case <-cancelCh:
			return
		}

		var wait time.Duration

		for {
			resCh <- result

			select {
			case <-cancelCh:
				return
			case <-time.After(wait):
				if wait == 0 {
					wait = 100 * time.Millisecond
				}
			}

			res, currentState, err := conf.Refresh()
			result = Result{
				Result: res,
				State:  currentState,
				Error:  err,
			}

			if err != nil {
				resCh <- result
				return
			}

			// If we're waiting for the absence of a thing, then return
			if res == nil && len(conf.Target) == 0 {
				targetOccurence++
				if conf.ContinuousTargetOccurence == targetOccurence {
					result.Done = true
					resCh <- result
					return
				}
				continue
			}

			if res == nil {
				notfoundTick++
				if notfoundTick > conf.NotFoundChecks {
					result.Error = &NotFoundError{
						LastError: err,
						Retries:   notfoundTick,
					}
					resCh <- result
					return
				}
			} else {
				notfoundTick = 0
				found := false

				for _, allowed := range conf.Target {
					if currentState == allowed {
						found = true
						targetOccurence++
						if conf.ContinuousTargetOccurence == targetOccurence {
							result.Done = true
							resCh <- result
							return
						}
						continue
					}
				}

				if slices.Contains(conf.Pending, currentState) {
					found = true
					targetOccurence = 0
				}

				if !found && len(conf.Pending) > 0 {
					result.Error = &UnexpectedStateError{
						LastError:     err,
						State:         result.State,
						ExpectedState: conf.Target,
					}
					resCh <- result
					return
				}
			}

			// Wait between refreshes using exponential backoff, except when
			// waiting for the target state to reoccur.
			if targetOccurence == 0 {
				wait *= 2
			}

			if conf.PollInterval > 0 && conf.PollInterval < 180*time.Second {
				wait = conf.PollInterval
			} else {
				if wait < conf.MinTimeout {
					wait = conf.MinTimeout
				} else if wait > 10*time.Second {
					wait = 10 * time.Second
				}
			}

			log.Printf("[TRACE] Waiting %s before next try", wait)
		}
	}()

	lastResult := Result{}

	timeout := time.After(conf.Timeout)
	for {
		select {
		case r, ok := <-resCh:
			if !ok {
				return lastResult.Result, lastResult.Error
			}

			if r.Done {
				return r.Result, r.Error
			}

			lastResult = r
		case <-ctx.Done():
			close(cancelCh)
			return nil, ctx.Err()
		case <-timeout:
			log.Printf("[WARN] WaitForState timeout after %s", conf.Timeout)
			log.Printf("[WARN] WaitForState starting %s refresh grace period", refreshGracePeriod)

			close(cancelCh)
			timeout := time.After(refreshGracePeriod)

			// The label lets a still-pending response be read before the
			// channel closes, rather than abandoning it on the first case.
		forSelect:
			for {
				select {
				case r, ok := <-resCh:
					if r.Done {
						return r.Result, r.Error
					}

					if !ok {
						break forSelect
					}

					lastResult = r
				case <-ctx.Done():
					log.Println("[ERROR] Context cancelation detected, abandoning grace period")
					break forSelect
				case <-timeout:
					log.Println("[ERROR] WaitForState exceeded refresh grace period")
					break forSelect
				}
			}

			return nil, &TimeoutError{
				LastError:     lastResult.Error,
				LastState:     lastResult.State,
				Timeout:       conf.Timeout,
				ExpectedState: conf.Target,
			}
		}
	}
}
