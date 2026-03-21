package daemon

import (
	"testing"
)

func TestKnownPatrols(t *testing.T) {
	patrols := KnownPatrols()
	if len(patrols) == 0 {
		t.Fatal("expected non-empty patrol list")
	}

	// Verify core patrols are present
	for _, name := range []string{"refinery", "witness", "deacon", "handler"} {
		if !IsKnownPatrol(name) {
			t.Errorf("expected %q to be a known patrol", name)
		}
	}

	// Verify opt-in patrols are present
	for _, name := range []string{"dolt_remotes", "dolt_backup", "doctor_dog"} {
		if !IsKnownPatrol(name) {
			t.Errorf("expected %q to be a known patrol", name)
		}
	}

	// Unknown patrol
	if IsKnownPatrol("nonexistent") {
		t.Error("expected nonexistent patrol to not be known")
	}
}

func TestSetPatrolEnabled(t *testing.T) {
	config := &DaemonPatrolConfig{
		Type:    "daemon-patrol-config",
		Version: 1,
	}

	// Enable an opt-in patrol
	if err := SetPatrolEnabled(config, "dolt_backup", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsPatrolEnabled(config, "dolt_backup") {
		t.Error("expected dolt_backup to be enabled after SetPatrolEnabled(true)")
	}

	// Disable it
	if err := SetPatrolEnabled(config, "dolt_backup", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if IsPatrolEnabled(config, "dolt_backup") {
		t.Error("expected dolt_backup to be disabled after SetPatrolEnabled(false)")
	}

	// Disable a core patrol
	if err := SetPatrolEnabled(config, "witness", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if IsPatrolEnabled(config, "witness") {
		t.Error("expected witness to be disabled after SetPatrolEnabled(false)")
	}

	// Re-enable it
	if err := SetPatrolEnabled(config, "witness", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !IsPatrolEnabled(config, "witness") {
		t.Error("expected witness to be enabled after SetPatrolEnabled(true)")
	}

	// Unknown patrol returns error
	if err := SetPatrolEnabled(config, "nonexistent", true); err == nil {
		t.Error("expected error for unknown patrol")
	}
}

func TestSetPatrolEnabled_AllPatrols(t *testing.T) {
	// Verify that SetPatrolEnabled works for every known patrol
	for _, name := range KnownPatrols() {
		t.Run(name, func(t *testing.T) {
			config := &DaemonPatrolConfig{
				Type:    "daemon-patrol-config",
				Version: 1,
			}

			// Enable
			if err := SetPatrolEnabled(config, name, true); err != nil {
				t.Fatalf("SetPatrolEnabled(%q, true): %v", name, err)
			}
			if !IsPatrolEnabled(config, name) {
				t.Errorf("expected %q to be enabled", name)
			}

			// Disable
			if err := SetPatrolEnabled(config, name, false); err != nil {
				t.Fatalf("SetPatrolEnabled(%q, false): %v", name, err)
			}
			if IsPatrolEnabled(config, name) {
				t.Errorf("expected %q to be disabled", name)
			}
		})
	}
}

func TestSetPatrolEnabled_SaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	config := &DaemonPatrolConfig{
		Type:    "daemon-patrol-config",
		Version: 1,
	}

	// Disable dolt_backup and enable doctor_dog
	_ = SetPatrolEnabled(config, "dolt_backup", false)
	_ = SetPatrolEnabled(config, "doctor_dog", true)
	_ = SetPatrolEnabled(config, "refinery", false)

	if err := SavePatrolConfig(tmpDir, config); err != nil {
		t.Fatalf("SavePatrolConfig: %v", err)
	}

	loaded := LoadPatrolConfig(tmpDir)
	if loaded == nil {
		t.Fatal("expected config to load")
	}

	if IsPatrolEnabled(loaded, "dolt_backup") {
		t.Error("expected dolt_backup to be disabled after save/load")
	}
	if !IsPatrolEnabled(loaded, "doctor_dog") {
		t.Error("expected doctor_dog to be enabled after save/load")
	}
	if IsPatrolEnabled(loaded, "refinery") {
		t.Error("expected refinery to be disabled after save/load")
	}
	// Unchanged patrol should keep default
	if !IsPatrolEnabled(loaded, "witness") {
		t.Error("expected witness to still be enabled (default)")
	}
}
