// Package beads provides delegation tracking for work units.
package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	beadsmod "github.com/steveyegge/beads"

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

// delegationMetadataKey is the key used to store delegation data in issue Metadata.
// bd slot was removed in v0.62; delegation is now stored in the metadata JSON field.
const delegationMetadataKey = "delegated_from"

// AddDelegation creates a delegation relationship from parent to child work unit.
// The delegation tracks who delegated (delegatedBy) and who received (delegatedTo),
// along with optional terms. Delegations enable credit cascade - when child work
// is completed, credit flows up to the parent work unit and its delegator.
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

	// Store delegation in child issue's metadata field under delegated_from key.
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return fmt.Errorf("setting delegation: %w", err)
	}
	actor := b.getActor()

	// Build metadata: merge with existing metadata if present
	existing, _ := store.GetIssue(ctx, d.Child)
	meta := make(map[string]json.RawMessage)
	if existing != nil && len(existing.Metadata) > 0 {
		_ = json.Unmarshal(existing.Metadata, &meta)
	}
	meta[delegationMetadataKey] = delegationJSON
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling delegation metadata: %w", err)
	}

	if err := store.UpdateIssue(ctx, d.Child, map[string]interface{}{
		"metadata": string(metaBytes),
	}, actor); err != nil {
		return fmt.Errorf("setting delegation metadata: %w", err)
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
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return fmt.Errorf("removing delegation: %w", err)
	}
	actor := b.getActor()

	// Remove delegated_from key from child issue's metadata
	existing, getErr := store.GetIssue(ctx, child)
	if getErr == nil && existing != nil {
		meta := make(map[string]json.RawMessage)
		if len(existing.Metadata) > 0 {
			_ = json.Unmarshal(existing.Metadata, &meta)
		}
		delete(meta, delegationMetadataKey)
		metaBytes, _ := json.Marshal(meta)
		_ = store.UpdateIssue(ctx, child, map[string]interface{}{
			"metadata": string(metaBytes),
		}, actor)
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
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting delegation: %w", err)
	}

	mi, err := store.GetIssue(ctx, child)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("getting issue: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("getting issue: %w", err)
	}

	if len(mi.Metadata) == 0 {
		return nil, nil
	}

	// Extract delegated_from key from metadata
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(mi.Metadata, &meta); err != nil {
		return nil, nil // Not a map — no delegation
	}

	raw, ok := meta[delegationMetadataKey]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var delegation Delegation
	if err := json.Unmarshal(raw, &delegation); err != nil {
		return nil, fmt.Errorf("parsing delegation: %w", err)
	}

	return &delegation, nil
}

// ListDelegationsFrom returns all delegations from a parent work unit.
// This searches for issues that have delegated_from in their metadata pointing to the parent.
func (b *Beads) ListDelegationsFrom(parent string) ([]*Delegation, error) {
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing delegations: %w", err)
	}

	// Use HasMetadataKey filter to find only issues that have delegation metadata
	mis, err := store.SearchIssues(ctx, "", beadsmod.IssueFilter{
		HasMetadataKey: delegationMetadataKey,
	})
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}

	var delegations []*Delegation
	for _, mi := range mis {
		if len(mi.Metadata) == 0 {
			continue
		}
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(mi.Metadata, &meta); err != nil {
			continue
		}
		raw, ok := meta[delegationMetadataKey]
		if !ok || len(raw) == 0 {
			continue
		}
		var d Delegation
		if err := json.Unmarshal(raw, &d); err != nil {
			continue
		}
		if d.Parent == parent {
			delegations = append(delegations, &d)
		}
	}

	return delegations, nil
}
