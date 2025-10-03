package ergon

// Manager provides unified access to all task management capabilities
type Manager struct {
	client     *Client
	inspector  TaskInspector
	controller TaskController
	batch      BatchController
	statistics TaskStatistics
	store      Store
}

// NewManager creates a new task manager
func NewManager(store Store, config ClientConfig) *Manager {
	return &Manager{
		client:     NewClient(store, config),
		inspector:  NewTaskInspector(store),
		controller: NewTaskController(store),
		batch:      NewBatchController(store),
		statistics: NewTaskStatistics(store),
		store:      store,
	}
}

// Client returns the task client for enqueueing
func (m *Manager) Client() *Client {
	return m.client
}

// Tasks returns the task inspector for querying tasks
func (m *Manager) Tasks() TaskInspector {
	return m.inspector
}

// Control returns the task controller for managing tasks
func (m *Manager) Control() TaskController {
	return m.controller
}

// Batch returns the batch controller for bulk operations
func (m *Manager) Batch() BatchController {
	return m.batch
}

// Stats returns the task statistics for analytics
func (m *Manager) Stats() TaskStatistics {
	return m.statistics
}

// Store returns the underlying store (for advanced operations)
func (m *Manager) Store() Store {
	return m.store
}

// Close closes the manager and underlying connections
func (m *Manager) Close() error {
	return m.store.Close()
}
