# gh-stacked Design

A GitHub CLI extension for managing stacked PRs from your commit history.

## Overview

**Core concept:** Your branch's commit history *is* the stack. Each commit becomes a separate PR. PRs chain together so reviewers see clean, incremental diffs.

**Why a gh extension?**
- Free auth (reuses `gh auth login`)
- Free GitHub API client (pagination, rate limits, retries)
- Easy distribution (`gh extension install`)
- Native feel (`gh stacked` vs standalone binary)

## Commands

| Command | Purpose |
|---------|---------|
| `gh stacked push` | Create or update stack of PRs from commits |
| `gh stacked amend N` | Amend PR N with staged changes |
| `gh stacked merge N` | Merge PR N, retarget & rebase rest |
| `gh stacked status` | Show stack state with PR links |

All commands support:
- `--help` with examples
- `--json` for machine-readable output

---

## Project Setup

### Create the extension

```bash
gh extension create --precompiled=go stacked
```

This scaffolds:
- Go project structure with `go.mod`
- GitHub Actions workflow for cross-platform builds
- Starter `main.go`

### Dependencies

```
github.com/cli/go-gh/v2    # gh auth, API, config
github.com/spf13/cobra     # CLI framework
```

Git operations: Shell out to `git` (not go-git) for predictability.

### Project structure

```
gh-stacked/
├── main.go              # entry point, root command
├── cmd/
│   ├── push.go          # push command
│   ├── amend.go         # amend command
│   ├── merge.go         # merge command
│   └── status.go        # status command
├── internal/
│   ├── git/             # git operations (exec wrappers)
│   ├── github/          # GitHub API operations
│   └── stack/           # stack logic (branch naming, PR tracking)
├── go.mod
├── go.sum
└── .github/workflows/   # release workflow (auto-generated)
```

---

## Stack Model

### Commit-to-PR Mapping

- **Default:** One commit = one PR
- **Override:** `--squash N-M` combines commits before pushing
- Squashing only allowed for unpushed commits (safety check with tests)

### Stack Identification

Branch-based: everything from divergence point with `main` to HEAD.

### Branch Naming Convention

Pattern: `stack/<parent-branch>/<NN>-<slug>`

Examples:
```
branch: feature/auth
stack:  stack/feature/auth/01-add-user-model
        stack/feature/auth/02-add-user-api

branch: feature/issue-123-something
stack:  stack/feature/issue-123-something/01-add-model
        stack/feature/issue-123-something/02-add-api
```

Slug: First 4-5 words of commit message, slugified.

### PR Structure

Chained branches - each PR targets the previous one:
```
PR #1 (01-add-user-model) → main
PR #2 (02-add-user-api)   → PR #1's branch
PR #3 (03-add-ui)         → PR #2's branch
```

When PR #1 merges, GitHub auto-retargets PR #2 to main.

### PR Body Footer

Each PR includes a stack footer:
```
---
📚 Stack: `feature/auth` (2/3) | ← [#42](url) | → [#44](url)
```

---

## Command Details

### `push`

Create or update stack of PRs from local commits.

**Flags:**
- `--preview` - show what would happen, no changes
- `--regroup` - interactive commit reorganization
- `--squash N-M` - squash commits N through M before pushing

**Workflow:**
1. Identify base branch (where current branch diverged from main)
2. List commits from base to HEAD
3. For each commit:
   - Create branch `stack/<parent-branch>/<NN>-<slug>`
   - Push commit to that branch
   - Create/update PR targeting previous branch (or main for first)
4. Add stack footer to each PR body

**Idempotent behavior:**
- New commits → create new PRs
- Changed commits → force-push updated branches
- Removed commits → close orphaned PRs (with confirmation)

**Squash validation:**
- Before squashing, check if commits already have PRs
- If any commits in range already pushed → error:
  ```
  Error: Cannot squash commits 2-3. They already have PRs:
    #2 → PR #43 (Add User API)
    #3 → PR #44 (Add validation)

  To combine these, close the PRs manually first, then re-push.
  ```
- **Lots of tests around this boundary**

**JSON output:**
```json
{
  "action": "created",
  "stack_id": "feature/auth",
  "prs_created": [42, 43, 44],
  "prs_updated": []
}
```

---

### `amend`

Apply staged changes to a specific PR in the stack.

**Usage:**
```bash
git add <files>
gh stacked amend 2    # amend PR 2
```

**Preconditions:**
- Staged changes must exist (error if none)
- PR number must be valid

**Workflow:**
1. Look up commit hash for PR N
2. Create fixup commit: `git commit --fixup=<hash>`
3. Run autosquash rebase: `git rebase -i --autosquash <base>`
4. Force-push all affected branches (PR N and all after it)
5. Update PR bodies if needed

**Conflict handling:**
If rebase conflicts, bail and restore:
```
Conflict rebasing "Add User API" onto updated base.
Restored original branch state.

To resolve manually:
  git rebase --onto <base> <old-base> HEAD
  # fix conflicts in <files>
  git rebase --continue
  gh stacked push
```

**JSON output:**
```json
{
  "amended": 2,
  "commit": "e2e2e2",
  "rebased": [3, 4],
  "pushed": ["stack/.../02-...", "stack/.../03-...", "stack/.../04-..."]
}
```

---

### `merge`

Merge a PR and update the remaining stack.

**Usage:**
```bash
gh stacked merge 1              # merge PR 1, retarget + rebase rest
gh stacked merge 1 --no-rebase  # just merge, leave rest alone
```

**Preconditions:**
- PR must be mergeable (checks passing, approved if required)
- Must be the first open PR in stack (can't merge PR 2 before PR 1)

**Workflow:**
1. Verify PR 1 is mergeable via GitHub API
2. Merge PR using GitHub API (squash merge)
3. Retarget PR 2 to `main`
4. Pull latest `main` locally
5. Rebase remaining local commits onto `main`
6. Force-push all remaining PR branches
7. Update PR body footers (PR 2 is now 1/2, etc.)

**Out-of-order protection:**
```
Error: Cannot merge PR #2. PR #1 must be merged first.

Stack order:
  #1 [OPEN] Add User model ← merge this first
  #2 [OPEN] Add User API   ← you tried to merge this
```

**Conflict handling:**
Same as amend - bail, restore, print guidance.

**JSON output:**
```json
{
  "merged": 42,
  "merge_commit": "abc123",
  "retargeted": [43, 44],
  "rebased": true,
  "remaining_prs": [43, 44]
}
```

---

### `status`

Show current stack state with actionable information.

**Usage:**
```bash
gh stacked status         # human-readable output
gh stacked status --json  # machine-readable
```

**Human output:**
```
Stack: feature/auth (3 PRs)

  #1 [MERGEABLE] Add User model
     PR #42: https://github.com/org/repo/pull/42
     → main | +120 -15 | 2 approvals, checks passing

  #2 [BLOCKED] Add User API
     PR #43: https://github.com/org/repo/pull/43
     → #42 | +85 -10 | changes requested by @reviewer

  #3 [WAITING] Add UI
     PR #44: https://github.com/org/repo/pull/44
     → #43 | +200 -30 | waiting on review

Local: in sync ✓
```

**States:**
- `MERGEABLE` - checks pass, approved, base is ready
- `BLOCKED` - changes requested or checks failing
- `WAITING` - pending review or waiting on earlier PR
- `MERGED` - already merged (shown for context, then removed)

**JSON output:**
```json
{
  "stack_id": "feature/auth",
  "base_branch": "main",
  "local_in_sync": true,
  "prs": [
    {
      "position": 1,
      "commit": "a1b2c3d",
      "title": "Add User model",
      "pr_number": 42,
      "pr_url": "https://github.com/org/repo/pull/42",
      "branch": "stack/feature/auth/01-add-user-model",
      "target_branch": "main",
      "state": "open",
      "mergeable": true,
      "checks_passing": true,
      "approvals": 2,
      "changes_requested": false,
      "additions": 120,
      "deletions": 15
    }
  ]
}
```

---

## Help Text

### Top-level

```
gh stacked --help
```
```
Manage stacked PRs from your commit history.

Each commit on your branch becomes a separate PR, chained together
so reviewers see clean, incremental diffs.

Commands:
  push      Create or update stack of PRs from commits
  amend     Amend a specific PR with staged changes
  merge     Merge a PR and update the remaining stack
  status    Show current stack state with PR links

Examples:
  # Create a stack from your branch
  gh stacked push

  # Preview what would be created
  gh stacked push --preview

  # Fix PR 2 after code review
  git add .
  gh stacked amend 2

  # Merge the first PR when ready
  gh stacked merge 1

  # Check stack state
  gh stacked status

Flags:
  --json    Output as JSON (available on all commands)
  --help    Show help for any command

Use "gh stacked <command> --help" for detailed command info.
```

### Per-command (example: push)

```
gh stacked push --help
```
```
Create or update a stack of PRs from commits on current branch.

Usage:
  gh stacked push [flags]

Flags:
  --preview       Show what would happen without making changes
  --regroup       Interactively reorganize commits before pushing
  --squash N-M    Squash commits N through M into single PR
  --json          Output result as JSON

Examples:
  # Push current commits as stacked PRs
  gh stacked push

  # Preview without creating PRs
  gh stacked push --preview

  # Squash commits 2-4 into one PR before pushing
  gh stacked push --squash 2-4
```

---

## Error Handling

### Conflict Strategy

When rebase conflicts occur:
1. Abort the rebase
2. Restore original branch state
3. Print exact git commands for manual resolution

Example output:
```
Conflict rebasing commit "Add User API" onto main.
Restored original branch state.

To resolve manually:
  git rebase main
  # fix conflicts
  git rebase --continue
  gh stacked push
```

### Validation Errors

Clear, actionable messages:
- "No staged changes. Stage files first with `git add`."
- "PR #2 not found in current stack."
- "Cannot squash commits 2-3. They already have PRs."
- "Cannot merge PR #2. PR #1 must be merged first."

---

## Claude Integration

**Design principles:**
- `--json` on all commands for machine parsing
- Rich status output with all decision-relevant info
- Predictable behavior (no interactive prompts in non-TTY)
- Clear exit codes

**Claude workflow:**
1. `gh stacked status --json` - understand current state
2. `gh stacked push --preview --json` - preview before pushing
3. Execute commands with `--json` for confirmation
4. Parse JSON to decide next action

---

## Testing Strategy

**Critical test areas:**
1. Squash validation - cannot squash already-pushed commits
2. Merge order enforcement - cannot merge out of order
3. Conflict recovery - state restored cleanly on failure
4. Idempotent push - re-running produces same result
5. Branch naming - preserves full parent path structure

**Test approach:**
- Unit tests for git/github wrappers
- Integration tests with real git repos (temp directories)
- Mock GitHub API for PR operations
