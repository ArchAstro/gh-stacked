# gh-stacked

A GitHub CLI extension for managing stacked pull requests.

Each commit on your branch becomes a separate PR, chained together so reviewers see clean, incremental diffs.

## Installation

### Prerequisites

- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated
- Go 1.21+ (for building from source)

### Install from source

```bash
git clone https://github.com/calvin/gh-stacked.git
cd gh-stacked
make install
```

### Verify installation

```bash
gh stacked --help
```

## Usage

### Create stacked PRs

From your feature branch with multiple commits:

```bash
gh stacked push
```

This will:
1. Create a branch for each commit (e.g., `stack/feature/auth/01-add-user-model`)
2. Push each branch to origin
3. Create a PR for each commit, chained together
4. Add navigation footer to each PR body

### Preview before pushing

```bash
gh stacked push --preview
```

Shows what branches and PRs would be created without making any changes.

### Check stack status

```bash
gh stacked status
```

Displays:
- Each PR with its position in the stack
- Merge status (PENDING, MERGEABLE, BLOCKED, WAITING)
- PR links and diff stats

### Amend a specific commit in the stack

After making changes to address code review feedback:

```bash
git add .
gh stacked amend 2
```

The argument is the stack position (1, 2, 3, ...) as shown by `gh stacked status`.

Alternatively, use the PR number directly with `--pr`:

```bash
git add .
gh stacked amend --pr 123
```

This will:
1. Create a fixup commit for the target commit
2. Run an autosquash rebase to fold it in
3. Force-push all affected branches

### Merge the next PR

When the first PR is approved and ready:

```bash
gh stacked merge
```

This will:
1. Merge the first open PR via GitHub API
2. Retarget the next PR to main
3. Rebase remaining commits onto main
4. Force-push all remaining branches

PRs are merged in stack order - the first open PR is always merged next.

To merge without rebasing the rest of the stack:

```bash
gh stacked merge --no-rebase
```

### Merge the entire stack at once

To squash all commits and merge as a single PR:

```bash
gh stacked merge-all
```

This will:
1. Squash all commits into one
2. Update and merge the last PR
3. Close all other PRs in the stack
4. Clean up all stack branches

## How it works

### Branch naming

Each commit gets a branch named:
```
stack/<parent-branch>/<NN>-<slug>
```

For example, if you're on `feature/auth` with commits "Add user model" and "Add user API":
```
stack/feature/auth/01-add-user-model
stack/feature/auth/02-add-user-api
```

### PR chaining

PRs are chained so each targets the previous one:
- PR #1: `stack/.../01-add-user-model` → `main`
- PR #2: `stack/.../02-add-user-api` → `stack/.../01-add-user-model`

This means reviewers see only the changes from that specific commit, not the cumulative diff.

### Stack footer

Each PR body includes a navigation footer:
```
---
📚 **Stack:** `feature/auth` (2/3) | ← #42 | → #44
```

## Commands

| Command | Description |
|---------|-------------|
| `gh stacked push` | Create or update stacked PRs from commits |
| `gh stacked push --preview` | Preview without creating PRs |
| `gh stacked push --squash 2-4` | Squash commits 2-4 into one PR |
| `gh stacked status` | Show current stack state |
| `gh stacked amend <position>` | Amend stack position with staged changes |
| `gh stacked amend --pr <num>` | Amend by PR number instead of position |
| `gh stacked merge` | Merge the first open PR and update the stack |
| `gh stacked merge --no-rebase` | Merge without rebasing remaining |
| `gh stacked merge-all` | Squash and merge entire stack as one PR |

## Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON (for scripting/automation) |
| `--help` | Show help for any command |

## Example workflow

```bash
# Create a feature branch
git checkout -b feature/user-auth

# Make commits (each will become a PR)
git commit -m "Add user model"
git commit -m "Add user validation"
git commit -m "Add user API endpoints"

# Push as stacked PRs
gh stacked push

# Check status
gh stacked status

# After code review, fix stack position 2
git add .
gh stacked amend 2

# When the first PR is approved, merge it
gh stacked merge

# Continue with the next PR, now targeting main
gh stacked status
```

## Development

```bash
make build         # Build the binary
make test          # Run tests
make test-verbose  # Run tests with verbose output
make cover         # Run tests with coverage summary
make cover-html    # Generate HTML coverage report
make install       # Build and install as gh extension
make uninstall     # Remove the extension
make clean         # Remove build artifacts
make fmt           # Format code
make lint          # Run linter (requires golangci-lint)
make dev           # Format, test, and build
make check         # Format, lint, and test (pre-commit check)
```

## Architecture

```
cmd/           # Cobra commands (push, status, amend, merge)
internal/
  git/         # Git operations wrapper
  github/      # GitHub API client
  stack/       # Stack logic (branch naming, PR entries)
```

## License

MIT
