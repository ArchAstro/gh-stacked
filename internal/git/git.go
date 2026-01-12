// internal/git/git.go
package git

import (
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
