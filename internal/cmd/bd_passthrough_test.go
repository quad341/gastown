package cmd

import "testing"

func TestFirstBeadIDArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple bead ID", []string{"gas-abc"}, "gas-abc"},
		{"bead with flags", []string{"--json", "gas-abc"}, "gas-abc"},
		{"flag with value then bead", []string{"--status", "open", "gas-abc"}, "gas-abc"},
		{"flag=value then bead", []string{"--status=open", "gas-abc"}, "gas-abc"},
		{"skip subcommand add", []string{"add", "gas-abc", "comment text"}, "gas-abc"},
		{"skip subcommand list", []string{"list", "gas-abc"}, "gas-abc"},
		{"no bead ID", []string{"--json", "--all"}, ""},
		{"empty args", []string{}, ""},
		{"just a subcommand", []string{"add"}, ""},
		{"multi-segment prefix", []string{"hq-cv-abc123"}, "hq-cv-abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstBeadIDArg(tt.args)
			if got != tt.want {
				t.Errorf("firstBeadIDArg(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestExtractRigFlag(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantRig    string
		wantFiltered []string
	}{
		{"no rig flag", []string{"--status=open"}, "", []string{"--status=open"}},
		{"--rig=value", []string{"--rig=gastown", "--json"}, "gastown", []string{"--json"}},
		{"--rig value", []string{"--rig", "gastown", "--json"}, "gastown", []string{"--json"}},
		{"rig at end", []string{"--json", "--rig", "MCDClient"}, "MCDClient", []string{"--json"}},
		{"empty args", []string{}, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRig, gotFiltered := extractRigFlag(tt.args)
			if gotRig != tt.wantRig {
				t.Errorf("extractRigFlag(%v) rig = %q, want %q", tt.args, gotRig, tt.wantRig)
			}
			if len(gotFiltered) != len(tt.wantFiltered) {
				t.Errorf("extractRigFlag(%v) filtered = %v, want %v", tt.args, gotFiltered, tt.wantFiltered)
			}
		})
	}
}
