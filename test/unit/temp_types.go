
type RetryWorker struct {
	ergon.WorkerDefaults[TestTaskArgs]
}

func (w *RetryWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
	return errors.New("test error")
}

func (w *RetryWorker) MaxRetries(task *ergon.Task[TestTaskArgs]) int {
	return 5
}

func (w *RetryWorker) RetryDelay(task *ergon.Task[TestTaskArgs], attempt int, err error) time.Duration {
	return time.Duration(attempt) * time.Second
}

type MiddlewareWorker struct {
	ergon.WorkerDefaults[TestTaskArgs]
	executed bool
}

func (w *MiddlewareWorker) Work(ctx context.Context, task *ergon.Task[TestTaskArgs]) error {
	w.executed = true
	return nil
}

func (w *MiddlewareWorker) Middleware() []ergon.MiddlewareFunc {
	return []ergon.MiddlewareFunc{
		func(next ergon.WorkerFunc) ergon.WorkerFunc {
			return func(ctx context.Context, task *ergon.InternalTask) error {
				return next(ctx, task)
			}
		},
	}
}
