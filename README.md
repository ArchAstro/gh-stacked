# gh-stacked

A GitHub CLI extension for managing stacked pull requests.

`gh-stacked` turns the commits on your feature branch into a stack of pull requests. Each commit maps to one PR, and each PR targets the previous PR in the stack so reviewers see small, incremental diffs instead of one large branch diff.

## Motivation

Stacked PRs are useful when one feature is too large for a single review, but splitting it into many long-lived branches is too much overhead.

`gh-stacked` is built for a commit-first workflow:

- Work on one normal feature branch.
- Keep commits small and reviewable.
- Turn those commits into chained PRs when you are ready for review.
- Amend a reviewed commit without manually rebuilding the stack.
- Merge one PR at a time, or squash the whole stack at the end.

## Installation

### Prerequisites

- [GitHub CLI](https://cli.github.com/) (`gh`) installed and authenticated
- `git`
- Go 1.23+ if you are building from source
- A repository whose base branch is `main` or `master`

### Install from source

```bash
git clone https://github.com/ArchAstro/gh-stacked.git
cd gh-stacked
make install
```

This installs the extension into `~/.local/share/gh/extensions/gh-stacked/`.

### Verify installation

```bash
gh stacked version
gh stacked --help
```

## How It Works

Run `gh stacked` from your feature branch, not from one of the generated stack branches.

Given a branch like `feature/auth` with commits:

```text
Add user model
Add user validation
Add user API
```

`gh stacked push` creates branches like:

```text
stack/feature/auth/01-add-user-model
stack/feature/auth/02-add-user-validation
stack/feature/auth/03-add-user-api
```

And PRs like:

- PR 1: `stack/.../01-add-user-model` -> `main`
- PR 2: `stack/.../02-add-user-validation` -> `stack/.../01-add-user-model`
- PR 3: `stack/.../03-add-user-api` -> `stack/.../02-add-user-validation`

That keeps each PR diff focused on one commit instead of the full feature branch.

## Typical Workflow

```bash
# Start a feature branch from main
git checkout -b feature/user-auth

# Make small commits; each commit becomes one PR
git commit -m "Add user model"
git commit -m "Add user validation"
git commit -m "Add user API endpoints"

# Preview the stack before creating anything
gh stacked push --mode=preview

# Create or update the stacked PRs
gh stacked push

# Inspect the stack
gh stacked status
gh stacked show

# Address review feedback on stack position 2
git add .
gh stacked amend 2

# Merge the first open PR in the stack
gh stacked merge
```

## Rebase A Stack From GitHub

`gh stacked rebase` updates a complete remote stack without changing the
caller's branch or worktree:

```bash
# Rebase the stack containing PR #123 onto the latest origin/main
gh stacked rebase --pr 123

# Run the complete topology and conflict preflight without pushing
gh stacked rebase --pr 123 --dry-run
```

The command validates the open PR base chain, rebases the cumulative stack tip
inside a temporary worktree, and reconstructs the one-commit-per-PR refs. It
only publishes after the whole rebase succeeds, using one atomic push with an
explicit force-with-lease for every old remote OID. A conflict, malformed
stack, or concurrent stack-ref update leaves all remote stack branches unchanged.
If the base branch moves during publication, the command detects it from the
remote and retries the complete stack up to three times. Repeated base movement
can leave the stack coherently based on the immediately previous base tip; the
command reports that state as a failure so it can be retried.

To expose `/stack rebase` on pull requests, copy
[`examples/rebase-stack-on-comment.yml`](examples/rebase-stack-on-comment.yml)
into the consuming repository's `.github/workflows` directory. Configure a
least-privilege GitHub App with:

- **Contents: read and write** to atomically update stack refs.
- **Pull requests: read and write** to discover the stack and post results.
- **Metadata: read** to verify the commenter's repository permission. GitHub
  Apps receive this permission automatically.

The example uses the App token for checkout, API calls, pushes, and comments so
normal pull-request CI runs after the rewritten refs are published. Before
enabling it, replace `REPLACE_WITH_FULL_COMMIT_SHA` with the reviewed
`gh-stacked` commit SHA. Do not pass a write-capable token to a mutable action
tag or branch. The workflow also applies a coarse trusted-association gate
before minting the App token; `gh stacked rebase --actor` remains the
authoritative repository write-permission check.

### Useful Variants

- `gh stacked push --mode=draft` creates the PRs as drafts.
- `gh stacked push --regroup` opens an interactive rebase before pushing so you can reorder or squash commits.
- `gh stacked push --squash 2-4` combines commits 2 through 4 into a single PR before push.
- `gh stacked amend --pr 123` targets a PR directly instead of using stack position.
- `gh stacked merge-all` squashes the full stack into one PR, merges it, and closes the rest.
- `gh stacked cleanup` removes dangling local commits and orphaned remote stack branches.
- `gh stacked housekeeping` shows commit/PR matching and highlights drift in the stack.

## Commands

| Command | Description |
|---------|-------------|
| `gh stacked push` | Create or update stacked PRs from commits on the current branch |
| `gh stacked push --mode=preview` | Preview the stack without creating or updating PRs |
| `gh stacked push --mode=draft` | Create stack PRs as drafts |
| `gh stacked push --regroup` | Interactively reorder or squash commits before pushing |
| `gh stacked push --squash 2-4` | Squash a commit range into one PR |
| `gh stacked status` | Show current stack state with mergeability and review details |
| `gh stacked show` | Show the stack top-to-bottom with PR titles and links |
| `gh stacked amend <position>` | Amend a stack position using staged changes |
| `gh stacked amend --pr <num>` | Amend by PR number instead of stack position |
| `gh stacked merge` | Merge the first open PR and rebase the remaining stack |
| `gh stacked merge --no-rebase` | Merge the first open PR without rebasing the remaining stack |
| `gh stacked rebase --pr <number>` | Rebase and atomically update a complete remote stack |
| `gh stacked rebase --pr <number> --dry-run` | Preflight a remote stack rebase without pushing |
| `gh stacked merge-all` | Squash and merge the entire stack as one PR |
| `gh stacked cleanup` | Remove dangling commits and orphaned stack branches |
| `gh stacked housekeeping` | Analyze local commits, remote branches, and PR state |
| `gh stacked version` | Print the current version |

## Tradeoffs And Shortcomings

`gh-stacked` is opinionated. Those opinions make the happy path simple, but they come with real costs:

- It is best suited to individual contributors or small teams working on one stack owner at a time.
- You do not work directly on `main`, but you do need to keep your feature branch rebased onto the latest base branch before `push` and `merge`. In a busy repository, that can mean frequent rebases.
- The tool intentionally rewrites history. `push`, `amend`, and `merge` update stack branches with force-pushes so the one-commit-to-one-PR mapping stays intact.
- Because of that rewriting, stack branches are not great shared collaboration branches and commit SHAs are not stable over time.
- It optimizes for clean review diffs, not for preserving the richest possible branch history. If long-lived immutable history is the priority, this workflow is a worse fit.
- Commit order and commit subjects matter. Reordering, squashing, or heavily rewriting a stack can change the commit-to-PR mapping and may require cleanup.
- Base branch detection currently assumes `origin/main`, `origin/master`, `main`, or `master`.

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
make check         # Format, lint, and test
```

## Architecture

```text
cmd/           # Cobra commands (push, status, amend, merge, cleanup)
internal/
  git/         # Git operations wrapper
  github/      # GitHub API client
  stack/       # Stack logic (branch naming, PR analysis, housekeeping)
```

## License

MIT
