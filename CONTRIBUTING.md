# Contributing to gh-stacked

Thanks for helping improve `gh-stacked`.

## Before You Start

- Search existing issues and pull requests before opening a duplicate.
- For a bug, include the `gh stacked version`, `gh version`, `git version`,
  operating system, the command you ran, and a minimal reproduction when
  possible.
- Discuss large behavior or architecture changes in an issue before investing
  in an implementation.
- Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Development

Install Go 1.25 or newer, Git, and GitHub CLI. Then run:

```bash
go mod download
make check
make build
./gh-stacked --help
```

Some integration tests create temporary local Git repositories. They do not
push to GitHub or modify the repository you run them from.

## Pull Requests

- Keep changes focused and add tests for behavior changes.
- Update user-facing documentation when commands or flags change.
- Run `make check` before submitting.
- Use clear commit subjects. Maintainers use rebase merges, so each commit
  should stand on its own and the pull request branch should be up to date.

Unless stated otherwise, contributions are licensed under the MIT License.
