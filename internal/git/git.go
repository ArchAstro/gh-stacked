// internal/git/git.go
package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Git struct{}

func New() *Git {
	return &Git{}
}

func (g *Git) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		// Include stderr in error message if available
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *Git) GetBaseBranch() (string, error) {
	// Prefer remote tracking branches (more up-to-date) over local branches
	for _, base := range []string{"origin/main", "origin/master", "main", "master"} {
		_, err := g.run("rev-parse", "--verify", base)
		if err == nil {
			return base, nil
		}
	}
	return "main", nil // Default fallback
}

func (g *Git) GetCurrentBranch() (string, error) {
	return g.run("rev-parse", "--abbrev-ref", "HEAD")
}

type Commit struct {
	Hash    string
	Subject string
	Body    string
}

func (g *Git) GetCommitsSinceBase(base string) ([]Commit, error) {
	// Get commits from base to HEAD, oldest first
	// Use a unique separator to split commits and their parts
	const recordSep = "---COMMIT_RECORD---"
	const fieldSep = "---FIELD_SEP---"

	// Format: hash<sep>subject<sep>body<record>
	format := "%H" + fieldSep + "%s" + fieldSep + "%b" + recordSep
	out, err := g.run("log", "--reverse", "--format="+format, base+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var commits []Commit
	records := strings.Split(out, recordSep)
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		parts := strings.Split(record, fieldSep)
		if len(parts) >= 2 {
			commit := Commit{
				Hash:    parts[0],
				Subject: parts[1],
			}
			if len(parts) >= 3 {
				commit.Body = strings.TrimSpace(parts[2])
			}
			commits = append(commits, commit)
		}
	}
	return commits, nil
}

func (g *Git) CreateBranchAtCommit(branchName, commitHash string) error {
	_, err := g.run("branch", "-f", branchName, commitHash)
	return err
}

func (g *Git) PushBranch(remote, branchName string, force bool) error {
	args := []string{"push", remote, branchName}
	if force {
		args = append(args, "--force-with-lease")
	}
	_, err := g.run(args...)
	return err
}

func (g *Git) CreateFixupCommit(targetHash string) error {
	_, err := g.run("commit", "--fixup="+targetHash, "--no-verify")
	return err
}

func (g *Git) AutosquashRebase(base string) error {
	// GIT_SEQUENCE_EDITOR=true makes rebase non-interactive
	cmd := exec.Command("git", "rebase", "-i", "--autosquash", base)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=true")
	// Connect stdout/stderr so we can see progress and any prompts
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// AutosquashRebaseQuiet runs autosquash rebase without output (for non-verbose mode)
func (g *Git) AutosquashRebaseQuiet(base string) error {
	cmd := exec.Command("git", "rebase", "-i", "--autosquash", base)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func (g *Git) InteractiveRebase(base string) error {
	// Fully interactive rebase - connects to terminal
	cmd := exec.Command("git", "rebase", "-i", base)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (g *Git) HasStagedChanges() (bool, error) {
	out, err := g.run("diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func (g *Git) AbortRebase() error {
	_, err := g.run("rebase", "--abort")
	return err
}

func (g *Git) GetOriginalHead() (string, error) {
	return g.run("rev-parse", "ORIG_HEAD")
}

func (g *Git) ResetHard(ref string) error {
	_, err := g.run("reset", "--hard", ref)
	return err
}

func (g *Git) Run(args ...string) (string, error) {
	return g.run(args...)
}

func (g *Git) ListRemoteBranches(pattern string) ([]string, error) {
	// List remote branches matching pattern (e.g., "origin/stack/feature/*")
	out, err := g.run("branch", "-r", "--list", pattern)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch != "" {
			// Remove "origin/" prefix if present
			branch = strings.TrimPrefix(branch, "origin/")
			branches = append(branches, branch)
		}
	}
	return branches, nil
}

func (g *Git) ApplyPatch(patch string, staged bool) error {
	// Apply a patch. If staged is true, apply to the index (staged changes).
	args := []string{"apply"}
	if staged {
		args = append(args, "--cached")
	}
	cmd := exec.Command("git", args...)
	cmd.Stdin = strings.NewReader(patch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}
