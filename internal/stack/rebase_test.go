package stack

import (
	"strings"
	"testing"
)

func TestBuildRemoteRebasePlanSortsAndValidatesChain(t *testing.T) {
	prs := []RemotePullRequest{
		{
			Number: 103,
			Branch: "stack/feature/auth/30-add-tests",
			Base:   "stack/feature/auth/20-add-api",
		},
		{
			Number: 101,
			Branch: "stack/feature/auth/10-add-model",
			Base:   "main",
		},
		{
			Number: 102,
			Branch: "stack/feature/auth/20-add-api",
			Base:   "stack/feature/auth/10-add-model",
		},
	}

	plan, err := BuildRemoteRebasePlan(102, "main", prs)
	if err != nil {
		t.Fatalf("BuildRemoteRebasePlan failed: %v", err)
	}

	if plan.StackID != "feature/auth" {
		t.Fatalf("expected stack ID feature/auth, got %q", plan.StackID)
	}
	if plan.Entries[0].PRNumber != 101 || plan.Entries[2].PRNumber != 103 {
		t.Fatalf("entries were not sorted by stack position: %#v", plan.Entries)
	}
	if plan.Tip().Branch != "stack/feature/auth/30-add-tests" {
		t.Fatalf("unexpected tip branch: %s", plan.Tip().Branch)
	}
}

func TestBuildRemoteRebasePlanRejectsBrokenBaseChain(t *testing.T) {
	prs := []RemotePullRequest{
		{Number: 101, Branch: "stack/feature/auth/01-add-model", Base: "main"},
		{Number: 102, Branch: "stack/feature/auth/02-add-api", Base: "main"},
	}

	_, err := BuildRemoteRebasePlan(101, "main", prs)
	if err == nil || !strings.Contains(err.Error(), "expected base") {
		t.Fatalf("expected broken-chain error, got %v", err)
	}
}

func TestBuildRemoteRebasePlanRejectsRequestedPROutsideStack(t *testing.T) {
	prs := []RemotePullRequest{
		{Number: 101, Branch: "stack/feature/auth/01-add-model", Base: "main"},
	}

	_, err := BuildRemoteRebasePlan(999, "main", prs)
	if err == nil || !strings.Contains(err.Error(), "PR #999") {
		t.Fatalf("expected requested-PR error, got %v", err)
	}
}

func TestBuildRemoteRebasePlanRejectsMixedStackIDs(t *testing.T) {
	prs := []RemotePullRequest{
		{Number: 101, Branch: "stack/feature/auth/01-add-model", Base: "main"},
		{Number: 102, Branch: "stack/feature/billing/02-add-api", Base: "stack/feature/auth/01-add-model"},
	}

	_, err := BuildRemoteRebasePlan(101, "main", prs)
	if err == nil || !strings.Contains(err.Error(), "different stack") {
		t.Fatalf("expected mixed-stack error, got %v", err)
	}
}
