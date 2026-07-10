# Contributing to schemix

Thank you for considering contributing to schemix! Here's how you can help.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/schemix.git`
3. Create a branch: `git checkout -b feat/your-feature`
4. Make your changes
5. Run tests: `go test ./...`
6. Commit with [Conventional Commits](https://www.conventionalcommits.org/): `git commit -m "feat: add something"`
7. Push and open a Pull Request

## Development

```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run benchmarks
go test -bench=. -benchmem ./...

# Run examples
cd example && go run .
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Exported symbols require doc comments
- Internal types/constants remain unexported
- Use table-driven tests
- ErrorCode constants use `Code` prefix (e.g. `CodeFormatMismatch`)

## Commit Convention

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `perf:` performance improvement
- `refactor:` code change that neither fixes a bug nor adds a feature
- `docs:` documentation only
- `test:` adding or updating tests
- `chore:` maintenance tasks

## Reporting Issues

- Use the GitHub issue templates
- Include Go version (`go version`)
- Include a minimal reproducible example
- Describe expected vs actual behavior

## Pull Request Guidelines

- One feature/fix per PR
- Include tests for new functionality
- Update documentation if API changes
- Ensure all tests pass before requesting review
- Keep PRs focused and small when possible
