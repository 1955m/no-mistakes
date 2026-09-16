package steps

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/agent"
	"github.com/kunchenguid/no-mistakes/internal/config"
)

// assertHostSearchBoundaryPrompt checks the host-search boundary delivered to a
// validation agent. It asserts on the emitted prompt - the generated interface
// the daemon hands a real agent - not on implementation source.
//
// The contract was added after a real Test run spent its whole 30-minute budget
// on an agent-authored `find / -maxdepth 4` that blocked at 0% CPU in a macOS
// directory-service automount under /home, so no scenario ever ran. The exact
// observed command shape must be disallowed, bounded repository/evidence reads
// must stay allowed, and the missing-tool fallback must be explicit.
func assertHostSearchBoundaryPrompt(t *testing.T, prompt string) {
	t.Helper()
	normalized := strings.Join(strings.Fields(prompt), " ")
	for _, want := range []string{
		// Whole-root searches are disallowed by name, including the observed shape.
		"Do not search the host filesystem",
		"Never run a filesystem-wide search such as `find /` or `mdfind /`",
		"never hunt the machine for an installed tool",
		"does not make a whole-root search bounded",
		// Bounded reads that must survive the new rule.
		"Bounded searches inside the worktree",
		"external evidence path a prompt explicitly names",
		"repository-local path you were given remain fine",
		// The missing-tool fallback is explicit, not a search.
		"not on PATH and no repository-local path is supplied",
		`report the affected scenario as "untested"`,
		"with the concrete reason naming the missing tool",
		"stop there",
	} {
		if !strings.Contains(normalized, want) {
			t.Errorf("emitted validation prompt missing host-search boundary %q:\n%s", want, prompt)
		}
	}
}

// TestReviewPromptCarriesBoundedHostSearchBoundary proves the Review prompt
// surface carries the host-search boundary from its authoritative owner,
// agent.WorktreeSteering, wired exactly as the daemon wires every pipeline
// agent. Without this the reviewer that gates the pushed branch can still wedge
// the run on an unbounded host search.
func TestReviewPromptCarriesBoundedHostSearchBoundary(t *testing.T) {
	t.Parallel()
	dir, baseSHA, headSHA := setupGitRepo(t)

	findingsJSON, _ := json.Marshal(cleanReviewFindings())
	inner := &mockAgent{
		name: "review",
		runFn: func(context.Context, agent.RunOpts) (*agent.Result, error) {
			return &agent.Result{Output: findingsJSON}, nil
		},
	}
	sctx := newTestContextWithDBRecords(t, inner, dir, baseSHA, headSHA, config.Commands{})
	// Same wiring as internal/daemon/manager.go: the run's agent is wrapped so
	// the workspace-boundary preamble leads every prompt it sends.
	sctx.Agent = agent.WithSteering(inner, sctx.EvidenceDir)

	if _, err := (&ReviewStep{}).Execute(sctx); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("expected 1 review call, got %d", len(inner.calls))
	}
	prompt := inner.calls[0].Prompt
	if !strings.HasPrefix(prompt, agent.WorktreeSteering(sctx.EvidenceDir)) {
		t.Fatalf("review prompt does not lead with the workspace-boundary preamble from agent.WorktreeSteering:\n%s", prompt)
	}
	assertHostSearchBoundaryPrompt(t, prompt)
}

// TestTestPromptCarriesBoundedHostSearchBoundary is the Test half of the same
// proof: the evidence turn that drives live scenarios, and that hung in the
// incident, must carry the boundary from the same owner.
func TestTestPromptCarriesBoundedHostSearchBoundary(t *testing.T) {
	t.Parallel()
	dir, baseSHA, headSHA := setupGitRepo(t)

	inner := &mockAgent{
		name: "test",
		runFn: func(context.Context, agent.RunOpts) (*agent.Result, error) {
			return &agent.Result{Output: json.RawMessage(passingScenarioFindingsJSON)}, nil
		},
	}
	sctx := newTestContextWithDBRecords(t, inner, dir, baseSHA, headSHA, config.Commands{})
	sctx.Agent = agent.WithSteering(inner, sctx.EvidenceDir)

	if _, err := (&TestStep{}).Execute(sctx); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("expected 1 evidence call, got %d", len(inner.calls))
	}
	prompt := inner.calls[0].Prompt
	if !strings.HasPrefix(prompt, agent.WorktreeSteering(sctx.EvidenceDir)) {
		t.Fatalf("test prompt does not lead with the workspace-boundary preamble from agent.WorktreeSteering:\n%s", prompt)
	}
	assertHostSearchBoundaryPrompt(t, prompt)
}
