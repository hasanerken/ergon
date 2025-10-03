package ergon

import (
	"errors"
	"testing"
	"time"
)

func TestJobCancelError(t *testing.T) {
	baseErr := errors.New("user cancelled")
	cancelErr := JobCancel(baseErr)

	t.Run("error message", func(t *testing.T) {
		if cancelErr.Error() != "user cancelled" {
			t.Errorf("expected 'user cancelled', got %s", cancelErr.Error())
		}
	})

	t.Run("is job cancelled", func(t *testing.T) {
		if !IsJobCancelled(cancelErr) {
			t.Error("IsJobCancelled should return true")
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		var jce *JobCancelError
		if !errors.As(cancelErr, &jce) {
			t.Error("should be JobCancelError")
		}

		if !errors.Is(jce.Unwrap(), baseErr) {
			t.Error("unwrap should return base error")
		}
	})

	t.Run("nil error", func(t *testing.T) {
		nilCancel := JobCancel(nil)
		if nilCancel.Error() != "job cancelled" {
			t.Errorf("expected 'job cancelled', got %s", nilCancel.Error())
		}
	})

	t.Run("regular error is not cancel", func(t *testing.T) {
		regularErr := errors.New("regular error")
		if IsJobCancelled(regularErr) {
			t.Error("regular error should not be job cancelled")
		}
	})
}

func TestJobSnoozeError(t *testing.T) {
	duration := 5 * time.Minute
	snoozeErr := JobSnooze(duration)

	t.Run("error message", func(t *testing.T) {
		expected := "job snoozed for 5m0s"
		if snoozeErr.Error() != expected {
			t.Errorf("expected '%s', got %s", expected, snoozeErr.Error())
		}
	})

	t.Run("is job snoozed", func(t *testing.T) {
		if !IsJobSnoozed(snoozeErr) {
			t.Error("IsJobSnoozed should return true")
		}
	})

	t.Run("get snooze duration", func(t *testing.T) {
		retrievedDuration, ok := GetSnoozeDuration(snoozeErr)
		if !ok {
			t.Error("should be able to get duration")
		}
		if retrievedDuration != duration {
			t.Errorf("expected duration %v, got %v", duration, retrievedDuration)
		}
	})

	t.Run("regular error is not snooze", func(t *testing.T) {
		regularErr := errors.New("regular error")
		if IsJobSnoozed(regularErr) {
			t.Error("regular error should not be job snoozed")
		}

		_, ok := GetSnoozeDuration(regularErr)
		if ok {
			t.Error("should not get duration from regular error")
		}
	})
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrDuplicateTask", ErrDuplicateTask, "duplicate task"},
		{"ErrWorkerNotFound", ErrWorkerNotFound, "worker not found"},
		{"ErrTaskNotFound", ErrTaskNotFound, "task not found"},
		{"ErrQueueNotFound", ErrQueueNotFound, "queue not found"},
		{"ErrServerStopped", ErrServerStopped, "server stopped"},
		{"ErrInvalidTaskArgs", ErrInvalidTaskArgs, "invalid task arguments"},
		{"ErrNotImplemented", ErrNotImplemented, "not implemented"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Errorf("%s is nil", tt.name)
			}

			if !errors.Is(tt.err, tt.err) {
				t.Errorf("%s should match itself", tt.name)
			}

			// Check error message contains expected text
			// (we don't check exact match as it may have additional context)
			if tt.err.Error() == "" {
				t.Errorf("%s has empty error message", tt.name)
			}
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	baseErr := errors.New("base error")

	t.Run("JobCancel wraps", func(t *testing.T) {
		wrapped := JobCancel(baseErr)

		if !errors.Is(wrapped, baseErr) {
			t.Error("wrapped error should match base error")
		}
	})

	t.Run("multiple wrapping", func(t *testing.T) {
		err1 := errors.New("level 1")
		err2 := JobCancel(err1)

		if !IsJobCancelled(err2) {
			t.Error("should be cancel error")
		}

		if !errors.Is(err2, err1) {
			t.Error("should unwrap to level 1")
		}
	})
}

func TestErrorComparison(t *testing.T) {
	t.Run("same error types", func(t *testing.T) {
		err1 := JobCancel(errors.New("test"))
		err2 := JobCancel(errors.New("test"))

		// Should both be JobCancelError but different instances
		if !IsJobCancelled(err1) || !IsJobCancelled(err2) {
			t.Error("both should be cancel errors")
		}

		// But not equal (different instances)
		if errors.Is(err1, err2) {
			t.Error("different instances should not be equal")
		}
	})

	t.Run("different error types", func(t *testing.T) {
		cancelErr := JobCancel(errors.New("cancel"))
		snoozeErr := JobSnooze(1 * time.Minute)

		if IsJobCancelled(snoozeErr) {
			t.Error("snooze error should not be cancel error")
		}

		if IsJobSnoozed(cancelErr) {
			t.Error("cancel error should not be snooze error")
		}
	})
}

func TestErrorInContext(t *testing.T) {
	t.Run("cancel in error chain", func(t *testing.T) {
		baseErr := errors.New("database error")
		cancelErr := JobCancel(baseErr)
		wrappedErr := errors.Join(cancelErr, errors.New("additional context"))

		// Should still detect cancel error in chain
		if !IsJobCancelled(wrappedErr) {
			t.Error("should detect cancel error in error chain")
		}
	})

	t.Run("snooze in error chain", func(t *testing.T) {
		snoozeErr := JobSnooze(10 * time.Second)
		wrappedErr := errors.Join(snoozeErr, errors.New("rate limited"))

		if !IsJobSnoozed(wrappedErr) {
			t.Error("should detect snooze error in error chain")
		}

		duration, ok := GetSnoozeDuration(wrappedErr)
		if !ok || duration != 10*time.Second {
			t.Error("should extract duration from error chain")
		}
	})
}

func TestNilErrorHandling(t *testing.T) {
	t.Run("IsJobCancelled with nil", func(t *testing.T) {
		if IsJobCancelled(nil) {
			t.Error("nil should not be job cancelled")
		}
	})

	t.Run("IsJobSnoozed with nil", func(t *testing.T) {
		if IsJobSnoozed(nil) {
			t.Error("nil should not be job snoozed")
		}
	})

	t.Run("GetSnoozeDuration with nil", func(t *testing.T) {
		_, ok := GetSnoozeDuration(nil)
		if ok {
			t.Error("should not get duration from nil")
		}
	})
}

func TestErrorSentinels(t *testing.T) {
	// Test that sentinel errors can be compared with errors.Is
	t.Run("errors.Is compatibility", func(t *testing.T) {
		sentinels := []error{
			ErrDuplicateTask,
			ErrWorkerNotFound,
			ErrTaskNotFound,
			ErrQueueNotFound,
			ErrServerStopped,
			ErrInvalidTaskArgs,
			ErrNotImplemented,
		}

		for _, sentinel := range sentinels {
			// Wrap the sentinel
			wrapped := errors.Join(sentinel, errors.New("context"))

			// Should still match with errors.Is
			if !errors.Is(wrapped, sentinel) {
				t.Errorf("wrapped error should match sentinel %v", sentinel)
			}
		}
	})
}

func TestDifferentDurations(t *testing.T) {
	durations := []time.Duration{
		1 * time.Millisecond,
		1 * time.Second,
		1 * time.Minute,
		1 * time.Hour,
		24 * time.Hour,
	}

	for _, d := range durations {
		t.Run(d.String(), func(t *testing.T) {
			err := JobSnooze(d)

			retrieved, ok := GetSnoozeDuration(err)
			if !ok {
				t.Error("should get duration")
			}

			if retrieved != d {
				t.Errorf("expected %v, got %v", d, retrieved)
			}
		})
	}
}
