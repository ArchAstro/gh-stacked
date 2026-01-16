// internal/git/mock.go
package git

// Verify MockGit implements GitOperations at compile time
var _ GitOperations = (*MockGit)(nil)

// MockGit is a test double for GitOperations
type MockGit struct {
	// State
	CurrentBranch  string
	BaseBranch     string
	Commits        []Commit
	StagedChanges  bool
	RemoteBranches []string // For ListRemoteBranches

	// Track calls for assertions
	Calls []MockCall

	// Error injection
	Errors map[string]error

	// Behavior hooks
	OnPushBranch   func(remote, branch string, force bool) error
	OnCreateBranch func(name, hash string) error
	OnRun          func(args []string) (string, error) // Custom Run handler for testing
}

type MockCall struct {
	Method string
	Args   []interface{}
}

func NewMockGit() *MockGit {
	return &MockGit{
		CurrentBranch: "feature/test",
		BaseBranch:    "main",
		Commits:       []Commit{},
		Errors:        make(map[string]error),
	}
}

func (m *MockGit) GetBaseBranch() (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetBaseBranch"})
	if err := m.Errors["GetBaseBranch"]; err != nil {
		return "", err
	}
	return m.BaseBranch, nil
}

func (m *MockGit) GetCurrentBranch() (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetCurrentBranch"})
	if err := m.Errors["GetCurrentBranch"]; err != nil {
		return "", err
	}
	return m.CurrentBranch, nil
}

func (m *MockGit) GetCommitsSinceBase(base string) ([]Commit, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetCommitsSinceBase", Args: []interface{}{base}})
	if err := m.Errors["GetCommitsSinceBase"]; err != nil {
		return nil, err
	}
	return m.Commits, nil
}

func (m *MockGit) HasStagedChanges() (bool, error) {
	m.Calls = append(m.Calls, MockCall{Method: "HasStagedChanges"})
	if err := m.Errors["HasStagedChanges"]; err != nil {
		return false, err
	}
	return m.StagedChanges, nil
}

func (m *MockGit) CreateBranchAtCommit(branchName, commitHash string) error {
	m.Calls = append(m.Calls, MockCall{Method: "CreateBranchAtCommit", Args: []interface{}{branchName, commitHash}})
	if m.OnCreateBranch != nil {
		return m.OnCreateBranch(branchName, commitHash)
	}
	if err := m.Errors["CreateBranchAtCommit"]; err != nil {
		return err
	}
	return nil
}

func (m *MockGit) PushBranch(remote, branchName string, force bool) error {
	m.Calls = append(m.Calls, MockCall{Method: "PushBranch", Args: []interface{}{remote, branchName, force}})
	if m.OnPushBranch != nil {
		return m.OnPushBranch(remote, branchName, force)
	}
	if err := m.Errors["PushBranch"]; err != nil {
		return err
	}
	return nil
}

func (m *MockGit) CreateFixupCommit(targetHash string) error {
	m.Calls = append(m.Calls, MockCall{Method: "CreateFixupCommit", Args: []interface{}{targetHash}})
	return m.Errors["CreateFixupCommit"]
}

func (m *MockGit) AutosquashRebase(base string) error {
	m.Calls = append(m.Calls, MockCall{Method: "AutosquashRebase", Args: []interface{}{base}})
	return m.Errors["AutosquashRebase"]
}

func (m *MockGit) InteractiveRebase(base string) error {
	m.Calls = append(m.Calls, MockCall{Method: "InteractiveRebase", Args: []interface{}{base}})
	return m.Errors["InteractiveRebase"]
}

func (m *MockGit) AbortRebase() error {
	m.Calls = append(m.Calls, MockCall{Method: "AbortRebase"})
	return m.Errors["AbortRebase"]
}

func (m *MockGit) GetOriginalHead() (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "GetOriginalHead"})
	return "abc123", m.Errors["GetOriginalHead"]
}

func (m *MockGit) ResetHard(ref string) error {
	m.Calls = append(m.Calls, MockCall{Method: "ResetHard", Args: []interface{}{ref}})
	return m.Errors["ResetHard"]
}

func (m *MockGit) Run(args ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Run", Args: []interface{}{args}})
	if m.OnRun != nil {
		return m.OnRun(args)
	}
	if err := m.Errors["Run"]; err != nil {
		return "", err
	}
	return "", nil
}

func (m *MockGit) ListRemoteBranches(pattern string) ([]string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "ListRemoteBranches", Args: []interface{}{pattern}})
	if err := m.Errors["ListRemoteBranches"]; err != nil {
		return nil, err
	}
	return m.RemoteBranches, nil
}

func (m *MockGit) ApplyPatch(patch string, staged bool) error {
	m.Calls = append(m.Calls, MockCall{Method: "ApplyPatch", Args: []interface{}{patch, staged}})
	return m.Errors["ApplyPatch"]
}

// WasCalled checks if method was called
func (m *MockGit) WasCalled(method string) bool {
	for _, c := range m.Calls {
		if c.Method == method {
			return true
		}
	}
	return false
}
