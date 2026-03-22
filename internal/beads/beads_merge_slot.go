// Package beads provides merge slot management for serialized conflict resolution.
package beads

import (
	"context"
	"fmt"
	"strings"

	beadsdk "github.com/steveyegge/beads"
)

// MergeSlotStatus represents the result of checking a merge slot.
type MergeSlotStatus struct {
	ID        string   `json:"id"`
	Available bool     `json:"available"`
	Holder    string   `json:"holder,omitempty"`
	Waiters   []string `json:"waiters,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// getMergeSlotID returns the merge slot bead ID for the current rig.
// The ID is derived from the issue_prefix database config (e.g., "gt-merge-slot").
func (b *Beads) getMergeSlotID(ctx context.Context, store beadsdk.Storage) string {
	prefix := "bd" // default
	if p, err := store.GetConfig(ctx, "issue_prefix"); err == nil && p != "" {
		prefix = strings.TrimSuffix(p, "-")
	}
	return prefix + "-merge-slot"
}

// MergeSlotCreate creates the merge slot bead for the current rig.
// The slot is used for serialized conflict resolution in the merge queue.
// Returns the slot ID if successful.
func (b *Beads) MergeSlotCreate() (string, error) {
	store, err := b.getLocalStore()
	if err != nil {
		return "", fmt.Errorf("creating merge slot: opening store: %w", err)
	}

	ctx := context.Background()
	slotID := b.getMergeSlotID(ctx, store)

	// Idempotent: if slot already exists, return its ID.
	if existing, err := store.GetIssue(ctx, slotID); err == nil && existing != nil {
		return slotID, nil
	}

	issue := &beadsdk.Issue{
		ID:          slotID,
		Title:       "Merge Slot",
		Description: "Exclusive access slot for serialized conflict resolution in the merge queue.",
		IssueType:   beadsdk.TypeTask,
		Status:      beadsdk.StatusOpen,
	}
	if err := store.CreateIssue(ctx, issue, b.getActor()); err != nil {
		return "", fmt.Errorf("creating merge slot: %w", err)
	}
	if err := store.AddLabel(ctx, slotID, "gt:slot", b.getActor()); err != nil {
		// Non-fatal: log but don't fail creation
		_ = err
	}

	return slotID, nil
}

// MergeSlotCheck checks the availability of the merge slot.
// Returns the current status including holder and waiters if held.
func (b *Beads) MergeSlotCheck() (*MergeSlotStatus, error) {
	store, err := b.getLocalStore()
	if err != nil {
		return nil, fmt.Errorf("checking merge slot: opening store: %w", err)
	}

	ctx := context.Background()
	slotID := b.getMergeSlotID(ctx, store)

	slot, err := store.GetIssue(ctx, slotID)
	if err != nil || slot == nil {
		return &MergeSlotStatus{ID: slotID, Error: "not found"}, nil
	}

	// The Assignee field tracks who currently holds the slot (replaces the
	// removed Holder field in bd v0.62).
	return &MergeSlotStatus{
		ID:        slot.ID,
		Available: slot.Status == beadsdk.StatusOpen,
		Holder:    slot.Assignee,
		Waiters:   slot.Waiters,
	}, nil
}

// MergeSlotAcquire attempts to acquire the merge slot for exclusive access.
// If holder is empty, defaults to the BD_ACTOR actor.
// If addWaiter is true and the slot is held, the requester is added to the waiters queue.
// Returns the acquisition result.
func (b *Beads) MergeSlotAcquire(holder string, addWaiter bool) (*MergeSlotStatus, error) {
	store, err := b.getLocalStore()
	if err != nil {
		return nil, fmt.Errorf("acquiring merge slot: opening store: %w", err)
	}

	ctx := context.Background()
	slotID := b.getMergeSlotID(ctx, store)

	if holder == "" {
		holder = b.getActor()
	}

	slot, err := store.GetIssue(ctx, slotID)
	if err != nil || slot == nil {
		return nil, fmt.Errorf("merge slot not found: %s (run MergeSlotCreate first)", slotID)
	}

	if slot.Status != beadsdk.StatusOpen {
		// Slot is held (status=in_progress, assignee=holder)
		if addWaiter {
			alreadyWaiting := false
			for _, w := range slot.Waiters {
				if w == holder {
					alreadyWaiting = true
					break
				}
			}
			if !alreadyWaiting {
				newWaiters := append(slot.Waiters, holder)
				if err := store.UpdateIssue(ctx, slot.ID, map[string]interface{}{
					"waiters": newWaiters,
				}, b.getActor()); err != nil {
					return nil, fmt.Errorf("adding to merge slot waiters: %w", err)
				}
				slot.Waiters = newWaiters
			}
		}
		return &MergeSlotStatus{
			ID:      slot.ID,
			Holder:  slot.Assignee,
			Waiters: slot.Waiters,
		}, nil
	}

	// Slot is available — acquire it by setting status=in_progress and assignee=holder.
	if err := store.UpdateIssue(ctx, slot.ID, map[string]interface{}{
		"status":   beadsdk.StatusInProgress,
		"assignee": holder,
	}, b.getActor()); err != nil {
		return nil, fmt.Errorf("acquiring merge slot: %w", err)
	}

	return &MergeSlotStatus{
		ID:        slot.ID,
		Available: true,
		Holder:    holder,
	}, nil
}

// MergeSlotRelease releases the merge slot after conflict resolution completes.
// If holder is provided, it verifies the slot is held by that holder before releasing.
func (b *Beads) MergeSlotRelease(holder string) error {
	store, err := b.getLocalStore()
	if err != nil {
		return fmt.Errorf("releasing merge slot: opening store: %w", err)
	}

	ctx := context.Background()
	slotID := b.getMergeSlotID(ctx, store)

	slot, err := store.GetIssue(ctx, slotID)
	if err != nil || slot == nil {
		return fmt.Errorf("merge slot not found: %s", slotID)
	}

	// Assignee tracks who holds the slot (replaces removed Holder field in bd v0.62).
	if holder != "" && slot.Assignee != holder {
		return fmt.Errorf("slot held by %s, not %s", slot.Assignee, holder)
	}

	if slot.Status == beadsdk.StatusOpen {
		// Already released
		return nil
	}

	if err := store.UpdateIssue(ctx, slot.ID, map[string]interface{}{
		"status":   beadsdk.StatusOpen,
		"assignee": "",
	}, b.getActor()); err != nil {
		return fmt.Errorf("releasing merge slot: %w", err)
	}

	return nil
}

// MergeSlotEnsureExists creates the merge slot if it doesn't exist.
// This is idempotent - safe to call multiple times.
func (b *Beads) MergeSlotEnsureExists() (string, error) {
	// Check if slot exists first
	status, err := b.MergeSlotCheck()
	if err != nil {
		return "", err
	}

	if status.Error == "not found" {
		// Create it
		return b.MergeSlotCreate()
	}

	return status.ID, nil
}
