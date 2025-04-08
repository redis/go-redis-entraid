# Contributing to go-redis-entraid

We welcome contributions from the community! If you'd like to contribute to this project, please follow these guidelines:

## Getting Started

1. Fork the repository
2. Create a new branch for your feature or bugfix
3. Make your changes
4. Run the tests and ensure they pass
5. Submit a pull request

## Development Setup

```bash
# Clone your fork
git clone https://github.com/your-username/go-redis-entraid.git
cd go-redis-entraid

# Install dependencies
go mod download

# Run tests
go test ./...
```

## Code Style and Standards

- Follow the Go standard formatting (`go fmt`)
- Write clear and concise commit messages
- Include tests for new features
- Update documentation as needed
- Follow the existing code style and patterns

## Testing

We maintain high test coverage for the project. When contributing:

- Add tests for new features
- Ensure existing tests pass
- Run the test coverage tool:
  ```bash
  go test -coverprofile=cover.out ./...
  go tool cover -html=cover.out
  ```

## Pull Request Process

1. Ensure your code passes all tests
2. Update the README.md if necessary
3. Submit your pull request with a clear description of the changes

## Reporting Issues

If you find a bug or have a feature request:

1. Check the existing issues to avoid duplicates
2. Create a new issue with:
   - A clear title and description
   - Steps to reproduce (for bugs)
   - Expected and actual behavior
   - Environment details (Go version, OS, etc.)

## Development Workflow

1. Create a new branch for your feature/fix:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit them:
   ```bash
   git add .
   git commit -m "Description of your changes"
   ```

3. Push your changes to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

4. Create a pull request from your fork to the main repository

## Review Process

- All pull requests will be reviewed by maintainers
- Be prepared to make changes based on feedback
- Ensure your code meets the project's standards
- Address any CI/CD failures

## Documentation

- Update relevant documentation when making changes
- Include examples for new features
- Update the README if necessary
- Add comments to complex code sections

## Questions?

If you have any questions about contributing, please:
1. Check the existing documentation
2. Look through existing issues
3. Create a new issue if your question hasn't been answered 