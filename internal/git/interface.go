// internal/git/interface.go
package git

// GitOperations defines all git operations used by commands
type GitOperations interface {
	GetBaseBranch() (string, error)
	GetCurrentBranch() (string, error)
	GetCommitsSinceBase(base string) ([]Commit, error)
	HasStagedChanges() (bool, error)
	CreateBranchAtCommit(branchName, commitHash string) error
	PushBranch(remote, branchName string, force bool) error
	CreateFixupCommit(targetHash string) error
	AutosquashRebase(base string) error
	InteractiveRebase(base string) error
	AbortRebase() error
	GetOriginalHead() (string, error)
	ResetHard(ref string) error
	Run(args ...string) (string, error)
	ListRemoteBranches(pattern string) ([]string, error)
	ApplyPatch(patch string, staged bool) error
	PrepareStackRebase(baseRef string, branches []string) (*StackRebaseResult, error)
	AtomicPushStackRefs(remote string, updates []RefUpdate) error
	RemoteRefOID(remote string, branch string) (string, error)
}

// Verify Git implements GitOperations at compile time
var _ GitOperations = (*Git)(nil)
