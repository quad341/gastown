// Package beads provides delegation tracking for work units.
package beads

import (
	"context"
	"encoding/json"
	"fmt"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/style"
)

// Delegation represents a work delegation relationship between work units.
// Delegation links a parent work unit to a child work unit, tracking who
// delegated the work and to whom, along with any terms of the delegation.
// This enables work distribution with credit cascade - work flows down,
// validation and credit flow up.
type Delegation struct {
	// Parent is the work unit ID that delegated the work
	Parent string `json:"parent"`

	// Child is the work unit ID that received the delegated work
	Child string `json:"child"`

	// DelegatedBy is the entity (hop:// URI or actor string) that delegated
	DelegatedBy string `json:"delegated_by"`

	// DelegatedTo is the entity (hop:// URI or actor string) receiving delegation
	DelegatedTo string `json:"delegated_to"`

	// Terms contains optional conditions of the delegation
	Terms *DelegationTerms `json:"terms,omitempty"`

	// CreatedAt is when the delegation was created
	CreatedAt string `json:"created_at,omitempty"`
}

// DelegationTerms holds optional terms/conditions for a delegation.
type DelegationTerms struct {
	// Portion describes what part of the parent work is delegated
	Portion string `json:"portion,omitempty"`

	// Deadline is the expected completion date
	Deadline string `json:"deadline,omitempty"`

	// AcceptanceCriteria describes what constitutes completion
	AcceptanceCriteria string `json:"acceptance_criteria,omitempty"`

	// CreditShare is the percentage of credit that flows to the delegate (0-100)
	CreditShare int `json:"credit_share,omitempty"`
}

// delegatedFromKey is the metadata key used to store delegation info on a child issue.
const delegatedFromKey = "delegated_from"

// AddDelegation creates a delegation relationship from parent to child work unit.
// The delegation tracks who delegated (delegatedBy) and who received (delegatedTo),
// along with optional terms. Delegations enable credit cascade - when child work
// is completed, credit flows up to the parent work unit and its delegator.
//
// Delegation info is stored as JSON in the child issue's Metadata field under
// the "delegated_from" key.
func (b *Beads) AddDelegation(d *Delegation) error {
	if d.Parent == "" || d.Child == "" {
		return fmt.Errorf("delegation requires both parent and child work unit IDs")
	}
	if d.DelegatedBy == "" || d.DelegatedTo == "" {
		return fmt.Errorf("delegation requires both delegated_by and delegated_to entities")
	}

	delegationJSON, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshaling delegation: %w", err)
	}

	store, err := b.getLocalStore()
	if err != nil {
		return fmt.Errorf("setting delegation: opening store: %w", err)
	}

	ctx := context.Background()
	mergedMeta, err := setMetadataKey(ctx, store, d.Child, delegatedFromKey, delegationJSON)
	if err != nil {
		return fmt.Errorf("setting delegation: %w", err)
	}

	if err := store.UpdateIssue(ctx, d.Child, map[string]interface{}{"metadata": mergedMeta}, b.getActor()); err != nil {
		return fmt.Errorf("setting delegation: %w", err)
	}

	// Also add a dependency so child blocks parent (work must complete before parent can close)
	if err := b.AddDependency(d.Parent, d.Child); err != nil {
		// Log but don't fail - the delegation is still recorded
		style.PrintWarning("could not add blocking dependency for delegation: %v", err)
	}

	return nil
}

// RemoveDelegation removes a delegation relationship.
func (b *Beads) RemoveDelegation(parent, child string) error {
	store, err := b.getLocalStore()
	if err != nil {
		return fmt.Errorf("removing delegation: opening store: %w", err)
	}

	ctx := context.Background()
	mergedMeta, err := removeMetadataKey(ctx, store, child, delegatedFromKey)
	if err != nil {
		return fmt.Errorf("removing delegation: %w", err)
	}

	if err := store.UpdateIssue(ctx, child, map[string]interface{}{"metadata": mergedMeta}, b.getActor()); err != nil {
		return fmt.Errorf("removing delegation: %w", err)
	}

	// Also remove the blocking dependency
	if err := b.RemoveDependency(parent, child); err != nil {
		// Log but don't fail
		style.PrintWarning("could not remove blocking dependency: %v", err)
	}

	return nil
}

// GetDelegation retrieves the delegation information for a child work unit.
// Returns nil if the issue has no delegation.
func (b *Beads) GetDelegation(child string) (*Delegation, error) {
	store, err := b.getLocalStore()
	if err != nil {
		return nil, fmt.Errorf("getting delegation: opening store: %w", err)
	}

	ctx := context.Background()
	issue, err := store.GetIssue(ctx, child)
	if err != nil {
		return nil, fmt.Errorf("getting issue: %w", err)
	}

	return parseDelegationFromMetadata(issue.Metadata), nil
}

// ListDelegationsFrom returns all delegations from a parent work unit.
// This searches for issues that have delegated_from pointing to the parent.
func (b *Beads) ListDelegationsFrom(parent string) ([]*Delegation, error) {
	store, err := b.getLocalStore()
	if err != nil {
		return nil, fmt.Errorf("listing delegations: opening store: %w", err)
	}

	// List all issues and check their delegation metadata.
	issues, err := b.List(ListOptions{Status: "all"})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}

	ctx := context.Background()
	var delegations []*Delegation
	for _, issue := range issues {
		full, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			continue
		}
		d := parseDelegationFromMetadata(full.Metadata)
		if d != nil && d.Parent == parent {
			delegations = append(delegations, d)
		}
	}

	return delegations, nil
}

// setMetadataKey returns updated metadata JSON with the given key set to value.
// Existing keys in the metadata are preserved.
func setMetadataKey(ctx context.Context, store beadsdk.Storage, issueID, key string, value json.RawMessage) (json.RawMessage, error) {
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("getting issue %s: %w", issueID, err)
	}
	meta := make(map[string]json.RawMessage)
	if issue.Metadata != nil {
		_ = json.Unmarshal(issue.Metadata, &meta) // best-effort; start fresh if not an object
	}
	meta[key] = value
	merged, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}
	return merged, nil
}

// removeMetadataKey returns updated metadata JSON with the given key removed.
// Existing keys in the metadata are preserved.
func removeMetadataKey(ctx context.Context, store beadsdk.Storage, issueID, key string) (json.RawMessage, error) {
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("getting issue %s: %w", issueID, err)
	}
	meta := make(map[string]json.RawMessage)
	if issue.Metadata != nil {
		_ = json.Unmarshal(issue.Metadata, &meta)
	}
	delete(meta, key)
	merged, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}
	return merged, nil
}

// parseDelegationFromMetadata extracts a Delegation from issue metadata JSON.
// Returns nil if no delegation is present.
func parseDelegationFromMetadata(metadata json.RawMessage) *Delegation {
	if metadata == nil {
		return nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return nil
	}
	raw, ok := meta[delegatedFromKey]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var d Delegation
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil
	}
	return &d
}
