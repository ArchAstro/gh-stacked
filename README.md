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
go build -o gh-stacked .
gh extension install .
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

### Amend a specific PR

After making changes to address code review feedback:

```bash
git add .
gh stacked amend 2
```

This will:
1. Create a fixup commit for PR #2's commit
2. Run an autosquash rebase to fold it in
3. Force-push all affected branches

### Merge a PR

When PR #1 is approved and ready:

```bash
gh stacked merge 1
```

This will:
1. Merge the PR via GitHub API
2. Retarget the next PR to main
3. Rebase remaining commits onto main
4. Force-push all remaining branches

PRs must be merged in order - you cannot merge PR #2 before PR #1.

To merge without rebasing the rest of the stack:

```bash
gh stacked merge 1 --no-rebase
```

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
| `gh stacked amend <n>` | Amend PR n with staged changes |
| `gh stacked merge <n>` | Merge PR n and update the stack |
| `gh stacked merge <n> --no-rebase` | Merge without rebasing remaining |

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

# After code review, fix PR #2
git add .
gh stacked amend 2

# When PR #1 is approved, merge it
gh stacked merge 1

# Continue with PR #2, now targeting main
gh stacked status
```

## Building

```bash
# Build
go build -o gh-stacked .

# Run tests
go test ./...

# Run tests with coverage
go test ./... -cover

# Install locally
cp gh-stacked ~/.local/share/gh/extensions/gh-stacked/
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
