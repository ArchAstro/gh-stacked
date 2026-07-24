package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/calvin/gh-stacked/internal/git"
	"github.com/calvin/gh-stacked/internal/github"
)

func setupRebaseCommandTest(t *testing.T) (*git.MockGit, *github.MockGitHub) {
	t.Helper()
	mockGit := git.NewMockGit()
	mockGit.BaseBranch = "origin/main"
	mockGit.RemoteBranches = []string{
		"stack/feature/auth/02-add-api",
		"stack/feature/auth/01-add-model",
	}
	mockGit.RebaseResult = &git.StackRebaseResult{
		BaseOID: "base-old",
		Commits: []git.Commit{
			{Hash: "new-model", Subject: "Add model"},
			{Hash: "new-api", Subject: "Add API"},
		},
		OldOIDs: map[string]string{
			"stack/feature/auth/01-add-model": "old-model",
			"stack/feature/auth/02-add-api":   "old-api",
		},
	}
	mockGit.RemoteRefOIDs["origin/main"] = "base-old"
	mockGit.RemoteRefOIDs["origin/stack/feature/auth/01-add-model"] = "old-model"
	mockGit.RemoteRefOIDs["origin/stack/feature/auth/02-add-api"] = "old-api"

	mockGH := github.NewMockGitHub()
	mockGH.AddExistingPRWithBase("stack/feature/auth/01-add-model", 101, "Add model", "open", false, "main")
	mockGH.AddExistingPRWithBase("stack/feature/auth/02-add-api", 102, "Add API", "open", false, "stack/feature/auth/01-add-model")

	oldGit := rebaseNewGit
	oldGitHub := rebaseNewGitHub
	oldPR := rebasePR
	oldActor := rebaseActor
	oldDryRun := rebaseDryRun
	oldRejectWorkflowChanges := rebaseRejectWorkflowChanges
	oldJSON := jsonOutput
	t.Cleanup(func() {
		rebaseNewGit = oldGit
		rebaseNewGitHub = oldGitHub
		rebasePR = oldPR
		rebaseActor = oldActor
		rebaseDryRun = oldDryRun
		rebaseRejectWorkflowChanges = oldRejectWorkflowChanges
		jsonOutput = oldJSON
	})

	rebaseNewGit = func() git.GitOperations { return mockGit }
	rebaseNewGitHub = func() (github.GitHubOperations, error) { return mockGH, nil }
	rebasePR = 102
	rebaseActor = ""
	rebaseDryRun = false
	rebaseRejectWorkflowChanges = false
	jsonOutput = false

	return mockGit, mockGH
}

func TestRunRebasePublishesValidatedStackAtomically(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("runRebase failed: %v", err)
	}

	if len(mockGit.AtomicUpdates) != 2 {
		t.Fatalf("expected 2 atomic updates, got %#v", mockGit.AtomicUpdates)
	}
	if mockGit.AtomicUpdates[0].Branch != "stack/feature/auth/01-add-model" || mockGit.AtomicUpdates[0].NewOID != "new-model" {
		t.Fatalf("unexpected first update: %#v", mockGit.AtomicUpdates[0])
	}
	if mockGit.AtomicUpdates[1].OldOID != "old-api" || mockGit.AtomicUpdates[1].NewOID != "new-api" {
		t.Fatalf("unexpected second update: %#v", mockGit.AtomicUpdates[1])
	}
}

func TestRunRebasePreparationFailureDoesNotPushAnyRef(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.Errors["PrepareStackRebase"] = errors.New("content conflict")

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "content conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("atomic push ran after rebase conflict")
	}
}

func TestRunRebasePublishesPrefixBeforeConflict(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.RebaseResult = &git.StackRebaseResult{
		BaseOID:       "base-old",
		Commits:       []git.Commit{{Hash: "new-model", Subject: "Add model"}},
		BlockedBranch: "stack/feature/auth/02-add-api",
		BlockedReason: "content conflict",
		OldOIDs: map[string]string{
			"stack/feature/auth/01-add-model": "old-model",
			"stack/feature/auth/02-add-api":   "old-api",
		},
	}

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("partial rebase failed: %v", err)
	}
	if len(mockGit.AtomicUpdates) != 1 {
		t.Fatalf("expected only the safe prefix update, got %#v", mockGit.AtomicUpdates)
	}
	if got := mockGit.AtomicUpdates[0]; got.Branch != "stack/feature/auth/01-add-model" || got.NewOID != "new-model" {
		t.Fatalf("unexpected prefix update: %#v", got)
	}
}

func TestRunRebaseRootConflictLeavesEveryRefUnchanged(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.RebaseResult = &git.StackRebaseResult{
		BaseOID:       "base-old",
		BlockedBranch: "stack/feature/auth/01-add-model",
		BlockedReason: "content conflict",
		OldOIDs: map[string]string{
			"stack/feature/auth/01-add-model": "old-model",
			"stack/feature/auth/02-add-api":   "old-api",
		},
	}

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("root conflict should be reported as a partial result: %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("root conflict published a stack ref")
	}
}

func TestRunRebaseReportsPublishedPrefixWhenBaseMoves(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.RebaseResult = &git.StackRebaseResult{
		BaseOID:       "base-old",
		Commits:       []git.Commit{{Hash: "new-model", Subject: "Add model"}},
		BlockedBranch: "stack/feature/auth/02-add-api",
		BlockedReason: "content conflict",
		OldOIDs: map[string]string{
			"stack/feature/auth/01-add-model": "old-model",
			"stack/feature/auth/02-add-api":   "old-api",
		},
	}
	mockGit.OnAtomicPushStackRefs = func(remote string, updates []git.RefUpdate) {
		mockGit.RemoteRefOIDs["origin/main"] = "base-new"
	}

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("published prefix was reported as a total failure: %v", err)
	}
	if len(mockGit.AtomicUpdates) != 1 {
		t.Fatalf("expected one safely published ref, got %#v", mockGit.AtomicUpdates)
	}
}

func TestRunRebaseDryRunDoesNotPushAnyRef(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	rebaseDryRun = true

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("dry-run pushed stack refs")
	}
}

func TestRunRebaseRejectsWorkflowChangesBeforePublishing(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	rebaseRejectWorkflowChanges = true
	mockGit.OnRun = func(args []string) (string, error) {
		if len(args) > 4 && args[0] == "diff-tree" && args[4] == "new-model" {
			return ".github/workflows/deploy.yml\n", nil
		}
		return "", nil
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot update workflow files") {
		t.Fatalf("expected workflow permission error, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("published refs for a stack containing workflow changes")
	}
}

func TestRunRebaseAllowsStacksWithoutWorkflowChangesWhenRestricted(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	rebaseRejectWorkflowChanges = true

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("ordinary stack was rejected: %v", err)
	}
	if !mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("ordinary stack was not published")
	}
}

func TestRunRebaseRejectsActorWithoutWritePermission(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	rebaseActor = "reader"
	mockGH.RepositoryPermissions["reader"] = "read"

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "write permission") {
		t.Fatalf("expected permission error, got %v", err)
	}
	if mockGit.WasCalled("PrepareStackRebase") {
		t.Fatal("prepared rebase for unauthorized actor")
	}
}

func TestRunRebaseRejectsBrokenPRChainBeforeGitRebase(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	prs := mockGH.PRs["stack/feature/auth/02-add-api"]
	prs[0].Base.Ref = "main"
	mockGH.PRs["stack/feature/auth/02-add-api"] = prs

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "expected base") {
		t.Fatalf("expected broken chain error, got %v", err)
	}
	if mockGit.WasCalled("PrepareStackRebase") {
		t.Fatal("prepared rebase for broken PR chain")
	}
}

func TestRunRebaseUsesRootPullRequestBase(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	rootPRs := mockGH.PRs["stack/feature/auth/01-add-model"]
	rootPRs[0].Base.Ref = "release/2026-07"
	mockGH.PRs["stack/feature/auth/01-add-model"] = rootPRs
	mockGit.RemoteRefOIDs["origin/release/2026-07"] = "base-old"

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("non-default base rebase failed: %v", err)
	}

	for _, call := range mockGit.Calls {
		if call.Method != "PrepareStackRebase" {
			continue
		}
		if got := call.Args[0].(string); got != "origin/release/2026-07" {
			t.Fatalf("expected root PR base, got %q", got)
		}
		return
	}
	t.Fatal("PrepareStackRebase was not called")
}

func TestRunRebaseRejectsForkPullRequest(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	prs := mockGH.PRs["stack/feature/auth/02-add-api"]
	prs[0].Head.Repo.FullName = "fork/repo"
	mockGH.PRs["stack/feature/auth/02-add-api"] = prs

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "comes from fork") {
		t.Fatalf("expected fork rejection, got %v", err)
	}
	if mockGit.WasCalled("PrepareStackRebase") {
		t.Fatal("prepared rebase for fork pull request")
	}
}

func TestRunRebaseRejectsMissingHeadRepository(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	prs := mockGH.PRs["stack/feature/auth/02-add-api"]
	prs[0].Head.Repo.FullName = ""
	mockGH.PRs["stack/feature/auth/02-add-api"] = prs

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "head repository is unavailable") {
		t.Fatalf("expected missing-repository rejection, got %v", err)
	}
	if mockGit.WasCalled("PrepareStackRebase") {
		t.Fatal("prepared rebase without a known head repository")
	}
}

func TestRunRebaseRejectsForkInSiblingPullRequest(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	prs := mockGH.PRs["stack/feature/auth/01-add-model"]
	prs[0].Head.Repo.FullName = "test/other-repo"
	mockGH.PRs["stack/feature/auth/01-add-model"] = prs

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "every branch") {
		t.Fatalf("expected sibling fork rejection, got %v", err)
	}
	if mockGit.WasCalled("PrepareStackRebase") {
		t.Fatal("prepared rebase with a fork-owned sibling")
	}
}

func TestRunRebaseAllowsActorWithWritePermission(t *testing.T) {
	_, mockGH := setupRebaseCommandTest(t)
	rebaseActor = "writer"
	mockGH.RepositoryPermissions["writer"] = "write"

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("writer was rejected: %v", err)
	}
}

func TestRunRebaseRechecksActorPermissionBeforePublishing(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	rebaseActor = "writer"
	mockGH.RepositoryPermissions["writer"] = "write"
	mockGit.OnPrepareStackRebase = func(baseRef string, branches []string) {
		mockGH.RepositoryPermissions["writer"] = "read"
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "write permission") {
		t.Fatalf("expected revoked permission error, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("published refs after actor permission was revoked")
	}
}

func TestRunRebaseRejectsPullRequestTopologyRace(t *testing.T) {
	mockGit, mockGH := setupRebaseCommandTest(t)
	mockGit.OnPrepareStackRebase = func(baseRef string, branches []string) {
		prs := mockGH.PRs["stack/feature/auth/01-add-model"]
		prs[0].Base.Ref = "release/raced"
		mockGH.PRs["stack/feature/auth/01-add-model"] = prs
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during rebase") {
		t.Fatalf("expected topology race error, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("published refs after PR topology changed")
	}
}

func TestRunRebaseRetriesVisibleBaseMovementBeforePublishing(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.OnPrepareStackRebase = func(baseRef string, branches []string) {
		mockGit.RemoteRefOIDs["origin/main"] = "base-new"
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "kept moving") {
		t.Fatalf("expected bounded base-race retry, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("published refs after observing the base move")
	}
}

func TestRunRebaseRejectsBlockedRefRaceBeforePublishingPrefix(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	mockGit.RebaseResult = &git.StackRebaseResult{
		BaseOID:       "base-old",
		Commits:       []git.Commit{{Hash: "new-model", Subject: "Add model"}},
		BlockedBranch: "stack/feature/auth/02-add-api",
		BlockedReason: "content conflict",
		OldOIDs: map[string]string{
			"stack/feature/auth/01-add-model": "old-model",
			"stack/feature/auth/02-add-api":   "old-api",
		},
	}
	mockGit.OnPrepareStackRebase = func(baseRef string, branches []string) {
		mockGit.RemoteRefOIDs["origin/stack/feature/auth/02-add-api"] = "raced-api"
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "stack ref") || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected blocked-ref race rejection, got %v", err)
	}
	if mockGit.WasCalled("AtomicPushStackRefs") {
		t.Fatal("published prefix after a blocked ref changed")
	}
}

func TestRunRebaseRetriesCompleteStackWhenBaseMovesDuringPublication(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	pushes := 0
	mockGit.OnAtomicPushStackRefs = func(remote string, updates []git.RefUpdate) {
		pushes++
		if pushes != 1 {
			return
		}
		mockGit.RemoteRefOIDs["origin/main"] = "base-new"
		mockGit.RebaseResult = &git.StackRebaseResult{
			BaseOID: "base-new",
			Commits: []git.Commit{
				{Hash: "newer-model", Subject: "Add model"},
				{Hash: "newer-api", Subject: "Add API"},
			},
			OldOIDs: map[string]string{
				"stack/feature/auth/01-add-model": "new-model",
				"stack/feature/auth/02-add-api":   "new-api",
			},
		}
	}

	if err := runRebase(nil, nil); err != nil {
		t.Fatalf("base-race retry failed: %v", err)
	}
	if pushes != 2 {
		t.Fatalf("expected two coherent stack publications, got %d", pushes)
	}
	if got := mockGit.AtomicUpdates[0].NewOID; got != "newer-model" {
		t.Fatalf("final stack was not rebased to moved base: %s", got)
	}
}

func TestRunRebaseFailsAfterThreeBaseRacesWithoutPartialStackState(t *testing.T) {
	mockGit, _ := setupRebaseCommandTest(t)
	pushes := 0
	mockGit.OnAtomicPushStackRefs = func(remote string, updates []git.RefUpdate) {
		pushes++
		nextBase := "base-race-" + string(rune('0'+pushes))
		mockGit.RemoteRefOIDs["origin/main"] = nextBase
		mockGit.RebaseResult = &git.StackRebaseResult{
			BaseOID: nextBase,
			Commits: []git.Commit{
				{Hash: "model-" + nextBase, Subject: "Add model"},
				{Hash: "api-" + nextBase, Subject: "Add API"},
			},
			OldOIDs: map[string]string{
				"stack/feature/auth/01-add-model": updates[0].NewOID,
				"stack/feature/auth/02-add-api":   updates[1].NewOID,
			},
		}
	}

	err := runRebase(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "kept moving") {
		t.Fatalf("expected bounded base-race failure, got %v", err)
	}
	if pushes != 3 {
		t.Fatalf("expected three complete publication attempts, got %d", pushes)
	}
	if len(mockGit.AtomicUpdates) != 2 {
		t.Fatalf("last publication was not a complete stack: %#v", mockGit.AtomicUpdates)
	}
}
