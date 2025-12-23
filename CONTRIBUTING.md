# Contributing to Vex

Thank you for your interest in contributing to Vex! This document provides guidelines and information for contributors.

## Getting Started

### Prerequisites

- Go 1.22 or later
- Git

### Setup

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/vex.git
   cd vex
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/uzqw/vex.git
   ```

## Development Workflow

### Building

```bash
make build
```

This will generate the following binaries in the `bin/` directory:
- `vex-server`: The main vector database server.
- `vex-benchmark`: The benchmarking and performance validation tool.

### Running Tests

```bash
make test
```

### Running Lints

```bash
make lint
```

### Formatting Code

```bash
make fmt
```

### Running All CI Checks Locally

Before submitting a pull request, **you must run all verification checks locally**:

```bash
make verify
```

This command runs:
- `go mod tidy` - Ensure dependencies are clean
- `go fmt` - Format code
- `go vet` - Static analysis
- `go test -race` - Tests with race detector
- `golangci-lint` - Comprehensive linting

## Pull Request Process

1. Create a new branch for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit them with clear, descriptive messages

3. **Run all verification checks locally before submitting**:
   ```bash
   make verify
   ```
   Your PR will not be merged if CI checks fail.

4. Push to your fork and create a Pull Request

5. Ensure the PR description clearly describes the problem and solution

## Code Style

- Follow standard Go conventions and idioms
- Run `go fmt` before committing
- Add tests for new functionality
- Keep functions small and focused
- Document exported functions and types

## Commit Messages

- Use the present tense ("Add feature" not "Added feature")
- Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
- Keep the first line under 50 characters
- Reference issues and pull requests where appropriate

## Reporting Bugs

When reporting bugs, please include:

- A clear and descriptive title
- Steps to reproduce the issue
- Expected behavior
- Actual behavior
- Go version (`go version`)
- Operating system and version

## Feature Requests

Feature requests are welcome! Please provide:

- A clear description of the feature
- The motivation or use case
- Any relevant examples or references

## Code of Conduct

Please note that this project follows a [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## License

By contributing to Vex, you agree that your contributions will be licensed under the Apache License 2.0.
