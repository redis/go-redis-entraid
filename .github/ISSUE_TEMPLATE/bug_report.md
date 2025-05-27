---
name: Bug report
about: Create a report to help us improve
title: "[BUG]"
labels: bug
assignees: ''
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this bug report!
  - type: textarea
    id: description
    attributes:
      label: Describe the bug
      description: A clear and concise description of what the bug is.
    validations:
      required: true
  - type: textarea
    id: reproduce
    attributes:
      label: To Reproduce
      description: Steps to reproduce the behavior
      placeholder: |
        1. Use function/method '...'
        2. With parameters '...'
        3. See error
    validations:
      required: true
  - type: textarea
    id: expected
    attributes:
      label: Expected behavior
      description: A clear and concise description of what you expected to happen.
    validations:
      required: true
  - type: textarea
    id: code
    attributes:
      label: Code Example
      description: If applicable, add a minimal code example to help explain your problem.
      render: go
      placeholder: |
        // Your code here
    validations:
      required: false
  - type: input
    id: go-version
    attributes:
      label: Go Version
      description: Which Go version are you using?
      placeholder: e.g., 1.21.0
    validations:
      required: true
  - type: input
    id: package-version
    attributes:
      label: Package version
      placeholder: e.g., v1.0.0
    validations:
      required: true
  - type: input
    id: go-redis-version
    attributes:
      label: go-redis Version
      description: Which version of go-redis are you using?
      placeholder: e.g., v9.0.5
    validations:
      required: true
  - type: input
    id: redis-server-version
    attributes:
      label: Redis Server Version
      description: Which Redis server version are you using?
      placeholder: e.g., 7.2.1
    validations:
      required: true
  - type: textarea
    id: environment
    attributes:
      label: Additional environment details
      description: Any other relevant environment information (OS, configuration, etc.)
      placeholder: More details about your environment
    validations:
      required: false
  - type: textarea
    id: context
    attributes:
      label: Additional context
      description: Add any other context about the problem here.
    validations:
      required: false
---
