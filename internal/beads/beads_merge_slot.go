// Package beads provides merge slot management for serialized conflict resolution.
package beads

import (
	"context"
	"fmt"
	"strings"

	beadsmod "github.com/steveyegge/beads"
)

// MergeSlotStatus represents the result of checking a merge slot.
type MergeSlotStatus struct {
	ID        string   `json:"id"`
	Available bool     `json:"available"`
	Holder    string   `json:"holder,omitempty"`
	Waiters   []string `json:"waiters,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// mergeSlotID returns the canonical merge-slot bead ID for this database.
// The ID is <issue_prefix>-merge-slot (e.g. "gt-merge-slot").
func (b *Beads) mergeSlotID(ctx context.Context, store beadsmod.Storage) (string, error) {
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		return "", fmt.Errorf("getting issue_prefix config: %w", err)
	}
	return prefix + "-merge-slot", nil
}

// MergeSlotCreate creates the merge slot bead for the current rig.
// The slot is used for serialized conflict resolution in the merge queue.
// Returns the slot ID if successful.
func (b *Beads) MergeSlotCreate() (string, error) {
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return "", fmt.Errorf("creating merge slot: %w", err)
	}

	slotID, err := b.mergeSlotID(ctx, store)
	if err != nil {
		return "", fmt.Errorf("creating merge slot: %w", err)
	}

	actor := b.getActor()
	mi := &beadsmod.Issue{
		ID:    slotID,
		Title: "Merge Slot",
	}
	mi.Labels = []string{"gt:slot"}

	if err := store.CreateIssue(ctx, mi, actor); err != nil {
		return "", fmt.Errorf("creating merge slot: %w", err)
	}

	return mi.ID, nil
}

// MergeSlotCheck checks the availability of the merge slot.
// Returns the current status including holder and waiters if held.
func (b *Beads) MergeSlotCheck() (*MergeSlotStatus, error) {
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking merge slot: %w", err)
	}

	slotID, err := b.mergeSlotID(ctx, store)
	if err != nil {
		return &MergeSlotStatus{Error: "not found"}, nil
	}

	mi, err := store.GetIssue(ctx, slotID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return &MergeSlotStatus{Error: "not found"}, nil
		}
		return nil, fmt.Errorf("checking merge slot: %w", err)
	}

	status := &MergeSlotStatus{
		ID:      mi.ID,
		Holder:  mi.Holder,
		Waiters: mi.Waiters,
	}
	status.Available = (mi.Holder == "")
	return status, nil
}

// MergeSlotAcquire attempts to acquire the merge slot for exclusive access.
// If holder is empty, defaults to BD_ACTOR environment variable.
// If addWaiter is true and the slot is held, the requester is added to the waiters queue.
// Returns the acquisition result.
func (b *Beads) MergeSlotAcquire(holder string, addWaiter bool) (*MergeSlotStatus, error) {
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring merge slot: %w", err)
	}

	actor := b.getActor()
	if holder == "" {
		holder = actor
	}

	slotID, err := b.mergeSlotID(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("acquiring merge slot: %w", err)
	}

	mi, err := store.GetIssue(ctx, slotID)
	if err != nil {
		return nil, fmt.Errorf("acquiring merge slot: %w", err)
	}

	// Slot is available — acquire it
	if mi.Holder == "" {
		if updateErr := store.UpdateIssue(ctx, slotID, map[string]interface{}{
			"holder": holder,
		}, actor); updateErr != nil {
			return nil, fmt.Errorf("acquiring merge slot: %w", updateErr)
		}
		return &MergeSlotStatus{
			ID:        slotID,
			Available: false,
			Holder:    holder,
		}, nil
	}

	// Slot is held — optionally add to waiters queue
	if addWaiter && holder != "" {
		waiters := mi.Waiters
		// Only add if not already in waiters
		alreadyWaiting := false
		for _, w := range waiters {
			if w == holder {
				alreadyWaiting = true
				break
			}
		}
		if !alreadyWaiting {
			waiters = append(waiters, holder)
			_ = store.UpdateIssue(ctx, slotID, map[string]interface{}{
				"waiters": waiters,
			}, actor)
			mi.Waiters = waiters
		}
	}

	return &MergeSlotStatus{
		ID:        slotID,
		Available: false,
		Holder:    mi.Holder,
		Waiters:   mi.Waiters,
	}, nil
}

// MergeSlotRelease releases the merge slot after conflict resolution completes.
// If holder is provided, it verifies the slot is held by that holder before releasing.
func (b *Beads) MergeSlotRelease(holder string) error {
	ctx := context.Background()
	store, err := b.openStore(ctx)
	if err != nil {
		return fmt.Errorf("releasing merge slot: %w", err)
	}

	actor := b.getActor()
	if holder == "" {
		holder = actor
	}

	slotID, err := b.mergeSlotID(ctx, store)
	if err != nil {
		return fmt.Errorf("releasing merge slot: %w", err)
	}

	mi, err := store.GetIssue(ctx, slotID)
	if err != nil {
		return fmt.Errorf("releasing merge slot: %w", err)
	}

	// Verify holder if specified
	if holder != "" && mi.Holder != holder {
		return fmt.Errorf("slot release failed: slot held by %q, not %q", mi.Holder, holder)
	}

	// Promote next waiter or clear holder
	updates := map[string]interface{}{"holder": ""}
	var newWaiters []string
	if len(mi.Waiters) > 0 {
		updates["holder"] = mi.Waiters[0]
		newWaiters = mi.Waiters[1:]
	}
	updates["waiters"] = newWaiters

	return store.UpdateIssue(ctx, slotID, updates, actor)
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
