package ergon

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrDuplicateTask is returned when a unique task already exists
	ErrDuplicateTask = errors.New("duplicate task: task with same unique key already exists")

	// ErrWorkerNotFound is returned when no worker is registered for a task kind
	ErrWorkerNotFound = errors.New("worker not found for task kind")

	// ErrTaskNotFound is returned when a task cannot be found
	ErrTaskNotFound = errors.New("task not found")

	// ErrQueueNotFound is returned when a queue cannot be found
	ErrQueueNotFound = errors.New("queue not found")

	// ErrServerStopped is returned when server is already stopped
	ErrServerStopped = errors.New("server stopped")

	// ErrInvalidTaskArgs is returned when task arguments are invalid
	ErrInvalidTaskArgs = errors.New("invalid task arguments")

	// ErrNotImplemented is returned when a feature is not implemented
	ErrNotImplemented = errors.New("not implemented")
)

// JobCancelError indicates a task should be cancelled (no retry)
type JobCancelError struct {
	Err error
}

func (e *JobCancelError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "job cancelled"
}

func (e *JobCancelError) Unwrap() error {
	return e.Err
}

// JobCancel wraps an error to indicate the task should be cancelled
func JobCancel(err error) error {
	return &JobCancelError{Err: err}
}

// IsJobCancelled checks if an error is a job cancellation
func IsJobCancelled(err error) bool {
	var jce *JobCancelError
	return errors.As(err, &jce)
}

// JobSnoozeError indicates a task should be snoozed (rescheduled without retry penalty)
type JobSnoozeError struct {
	Duration time.Duration
}

func (e *JobSnoozeError) Error() string {
	return fmt.Sprintf("job snoozed for %v", e.Duration)
}

// JobSnooze reschedules a task for later without counting as a retry
func JobSnooze(duration time.Duration) error {
	return &JobSnoozeError{Duration: duration}
}

// IsJobSnoozed checks if an error is a job snooze
func IsJobSnoozed(err error) bool {
	var jse *JobSnoozeError
	return errors.As(err, &jse)
}

// GetSnoozeDuration extracts the snooze duration from an error
func GetSnoozeDuration(err error) (time.Duration, bool) {
	var jse *JobSnoozeError
	if errors.As(err, &jse) {
		return jse.Duration, true
	}
	return 0, false
}
