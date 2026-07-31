# Changelog

This project follows [Semantic Versioning](https://semver.org/).

## Unreleased

### Added

- A composite GitHub Action and `gh stacked rebase` command for guarded,
  stack-wide remote rebases.

### Changed

- The public Go module path is now `github.com/ArchAstro/gh-stacked`.

## 0.1.1 - 2026-02-03

### Added

- `gh stacked version` with semantic-version output.
- A `stack/last` label to support CI optimization on the final pull request in
  a stack.
