// internal/git/git.go
package git

import (
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
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *Git) GetBaseBranch() (string, error) {
	// Try common base branches in order of preference
	for _, base := range []string{"main", "master"} {
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
}

func (g *Git) GetCommitsSinceBase(base string) ([]Commit, error) {
	// Get commits from base to HEAD, oldest first
	out, err := g.run("log", "--reverse", "--format=%H %s", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var commits []Commit
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			commits = append(commits, Commit{
				Hash:    parts[0],
				Subject: parts[1],
			})
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
	_, err := g.run("commit", "--fixup="+targetHash)
	return err
}

func (g *Git) AutosquashRebase(base string) error {
	// GIT_SEQUENCE_EDITOR=true makes rebase non-interactive
	cmd := exec.Command("git", "rebase", "-i", "--autosquash", base)
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=true")
	return cmd.Run()
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
