package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type StackRebaseResult struct {
	Commits       []Commit
	OldOIDs       map[string]string
	BaseOID       string
	BlockedBranch string
	BlockedReason string
}

type RefUpdate struct {
	Branch string
	OldOID string
	NewOID string
}

func runGitInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func commitsInRange(dir, revisionRange string) ([]Commit, error) {
	// Git commit messages cannot contain NUL, so -z gives us an unambiguous
	// machine-readable record boundary even when a message contains text that
	// resembles a human-readable separator.
	out, err := runGitInDir(dir, "log", "-z", "--reverse", "--format=%H%x00%s%x00%b", revisionRange)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	commits := make([]Commit, 0, len(fields)/3)
	for len(fields) >= 3 {
		hash := strings.TrimSpace(fields[0])
		subject := fields[1]
		body := strings.TrimSpace(fields[2])
		fields = fields[3:]
		if hash == "" {
			continue
		}
		commits = append(commits, Commit{Hash: hash, Subject: subject, Body: body})
	}
	return commits, nil
}

func (g *Git) PrepareStackRebase(baseRef string, branches []string) (*StackRebaseResult, error) {
	if len(branches) == 0 {
		return nil, fmt.Errorf("cannot rebase an empty stack")
	}

	baseOID, err := g.run("rev-parse", "--verify", baseRef)
	if err != nil {
		return nil, fmt.Errorf("resolve base ref %s: %w", baseRef, err)
	}

	oldOIDs := make(map[string]string, len(branches))
	previousOID := ""
	for i, branch := range branches {
		remoteRef := "origin/" + branch
		oid, err := g.run("rev-parse", "--verify", remoteRef)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", remoteRef, err)
		}
		oldOIDs[branch] = oid

		parents, err := g.run("rev-list", "--parents", "-n", "1", remoteRef)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", remoteRef, err)
		}
		parentFields := strings.Fields(parents)
		if len(parentFields) != 2 {
			return nil, fmt.Errorf("stack branch %s must point to a single-parent commit", branch)
		}

		if i == 0 {
			mergeBase, err := g.run("merge-base", baseRef, remoteRef)
			if err != nil {
				return nil, fmt.Errorf("find merge base for %s: %w", branch, err)
			}
			countText, err := g.run("rev-list", "--count", mergeBase+".."+remoteRef)
			if err != nil {
				return nil, fmt.Errorf("count root commits for %s: %w", branch, err)
			}
			count, _ := strconv.Atoi(strings.TrimSpace(countText))
			if count != 1 {
				return nil, fmt.Errorf("root stack branch %s contains %d commits; expected exactly 1", branch, count)
			}
		} else if parentFields[1] != previousOID {
			return nil, fmt.Errorf("stack branch %s is not directly based on previous stack ref", branch)
		}
		previousOID = oid
	}

	parentDir, err := os.MkdirTemp("", "gh-stacked-rebase-")
	if err != nil {
		return nil, fmt.Errorf("create temporary rebase directory: %w", err)
	}
	defer os.RemoveAll(parentDir)

	worktreeDir := filepath.Join(parentDir, "worktree")
	if _, err := g.run("worktree", "add", "--detach", worktreeDir, baseRef); err != nil {
		return nil, fmt.Errorf("create temporary worktree: %w", err)
	}
	defer func() {
		_, _ = g.run("worktree", "remove", "--force", worktreeDir)
		_, _ = g.run("worktree", "prune")
	}()

	result := &StackRebaseResult{OldOIDs: oldOIDs, BaseOID: baseOID}
	for _, branch := range branches {
		if _, err := runGitInDir(worktreeDir,
			"-c", "user.name=gh-stacked",
			"-c", "user.email=gh-stacked@users.noreply.github.com",
			"cherry-pick", "--allow-empty", "--keep-redundant-commits", oldOIDs[branch],
		); err != nil {
			unmerged, inspectErr := runGitInDir(worktreeDir, "diff", "--name-only", "--diff-filter=U")
			_, _ = runGitInDir(worktreeDir, "cherry-pick", "--abort")
			if inspectErr != nil {
				return nil, fmt.Errorf("inspect failed stack commit %s: %w", branch, inspectErr)
			}
			if strings.TrimSpace(unmerged) == "" {
				return nil, fmt.Errorf("replay stack commit %s: %w", branch, err)
			}
			commits, readErr := commitsInRange(worktreeDir, baseRef+"..HEAD")
			if readErr != nil {
				return nil, fmt.Errorf("read partially rebased commits: %w", readErr)
			}
			result.Commits = commits
			result.BlockedBranch = branch
			result.BlockedReason = fmt.Sprintf("stack rebase conflicts with %s: %v", baseRef, err)
			return result, nil
		}
	}

	commits, err := commitsInRange(worktreeDir, baseRef+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("read rebased commits: %w", err)
	}
	if len(commits) != len(branches) {
		return nil, fmt.Errorf("rebased stack has %d commits for %d pull requests", len(commits), len(branches))
	}

	result.Commits = commits
	return result, nil
}

func (g *Git) AtomicPushStackRefs(remote string, updates []RefUpdate) error {
	if len(updates) == 0 {
		return fmt.Errorf("cannot push an empty stack update")
	}

	args := []string{"push", "--atomic"}
	for _, update := range updates {
		if update.Branch == "" || update.OldOID == "" || update.NewOID == "" {
			return fmt.Errorf("invalid stack ref update for branch %q", update.Branch)
		}
		args = append(args, "--force-with-lease=refs/heads/"+update.Branch+":"+update.OldOID)
	}
	args = append(args, remote)
	for _, update := range updates {
		args = append(args, update.NewOID+":refs/heads/"+update.Branch)
	}

	if _, err := g.run(args...); err != nil {
		return fmt.Errorf("atomic stack push failed: %w", err)
	}
	return nil
}

func (g *Git) RemoteRefOID(remote string, branch string) (string, error) {
	if remote == "" || branch == "" {
		return "", fmt.Errorf("remote and branch are required")
	}
	out, err := g.run("ls-remote", "--exit-code", remote, "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("resolve remote ref %s/%s: %w", remote, branch, err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch {
		return "", fmt.Errorf("unexpected ls-remote response for %s/%s: %q", remote, branch, out)
	}
	return fields[0], nil
}
