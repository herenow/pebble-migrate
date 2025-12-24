# Contributing to pebble-migrate

Thank you for your interest in contributing to pebble-migrate!

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR-USERNAME/pebble-migrate.git`
3. Create a branch: `git checkout -b feature/your-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Commit: `git commit -m "Add your feature"`
7. Push: `git push origin feature/your-feature`
8. Open a Pull Request

## Development Setup

```bash
# Clone
git clone https://github.com/herenow/pebble-migrate.git
cd pebble-migrate

# Install dependencies
go mod download

# Run tests
go test ./...

# Build CLI
go build -o pebble-migrate ./cmd/pebble-migrate
```

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Run `go vet` to check for issues
- Add tests for new functionality
- Update documentation as needed

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific test
go test -run TestMigrationOrdering ./...

# Run with race detector
go test -race ./...
```

## Pull Request Guidelines

1. **Clear Description**: Explain what your PR does and why
2. **Tests**: Add tests for new functionality
3. **Documentation**: Update docs if needed
4. **Single Purpose**: Keep PRs focused on one thing
5. **Clean History**: Rebase if needed for clean commits

## Reporting Issues

When reporting issues, please include:
- Go version (`go version`)
- pebble-migrate version
- Operating system
- Steps to reproduce
- Expected vs actual behavior
- Error messages if any

## Code of Conduct

Be respectful and constructive in all interactions.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
