# ⚡ Ergon

**Ergon** (ἔργον, Greek: "work, task, deed") - A distributed task queue system for Go with persistence, at-least-once delivery, and multiple storage backends.

## 🎯 Features

- ✅ **Type-safe tasks** - Generic task definitions with compile-time safety
- 💾 **Multiple backends** - PostgreSQL and BadgerDB (embedded) support
- 🔄 **At-least-once delivery** - Guaranteed task execution with retries
- 📦 **Batch operations** - Efficient bulk task enqueueing
- 🎛️ **Task management** - Pause, cancel, retry, and inspect tasks
- 📊 **Statistics** - Built-in monitoring and queue statistics
- ⚙️ **Worker pools** - Configurable concurrency per queue
- 🔌 **Middleware** - Hook into task lifecycle (before/after processing)
- 🏷️ **Task groups** - Group related tasks for batch operations
- ⏱️ **Scheduling** - Schedule tasks for future execution
- 🚦 **Queue control** - Pause/resume queues dynamically

## 📦 Installation

```bash
go get github.com/hasanerken/ergon
```

## 🚀 Quick Start

See `examples/` directory for complete examples.

## 📚 Documentation

Detailed documentation is available in the `docs/` directory:

- **[Design Patterns](docs/DESIGN_PATTERNS.md)** - Architecture and patterns used in Ergon
- **[Monitoring Guide](docs/MONITORING_GUIDE.md)** - How to monitor and observe task queues
- **[Monitoring Architecture](docs/MONITORING_ARCHITECTURE.md)** - Monitoring system design
- **[Scheduling Guide](docs/SCHEDULING_GUIDE.md)** - Task scheduling and recurring tasks
- **[Test Suite](TEST_README.md)** - Comprehensive testing documentation

## 📄 License

MIT License

## 🙏 Credits

Built with ⚡ by [@hasanerken](https://github.com/hasanerken)
