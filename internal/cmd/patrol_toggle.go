package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/daemon"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

var patrolDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a patrol by name",
	Long: `Disable a specific patrol so it skips execution entirely.
The change persists across daemon restarts (stored in mayor/daemon.json).

The daemon must be restarted for the change to take effect on patrols
that are already running.

Examples:
  gt patrol disable dolt_backup
  gt patrol disable scheduled_maintenance`,
	Args: cobra.ExactArgs(1),
	RunE: runPatrolDisable,
}

var patrolEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a patrol by name",
	Long: `Enable a previously disabled patrol.
The change persists across daemon restarts (stored in mayor/daemon.json).

The daemon must be restarted for the change to take effect.

Examples:
  gt patrol enable dolt_backup
  gt patrol enable scheduled_maintenance`,
	Args: cobra.ExactArgs(1),
	RunE: runPatrolEnable,
}

var patrolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all patrols with their enabled/disabled status",
	Long: `Show all known patrols and whether they are currently enabled or disabled.

Core patrols (refinery, witness, deacon, handler) default to enabled.
Opt-in patrols default to disabled unless explicitly enabled.

Examples:
  gt patrol list`,
	RunE: runPatrolList,
}

func init() {
	patrolCmd.AddCommand(patrolDisableCmd)
	patrolCmd.AddCommand(patrolEnableCmd)
	patrolCmd.AddCommand(patrolListCmd)
}

func runPatrolDisable(cmd *cobra.Command, args []string) error {
	return setPatrolState(args[0], false)
}

func runPatrolEnable(cmd *cobra.Command, args []string) error {
	return setPatrolState(args[0], true)
}

func setPatrolState(name string, enabled bool) error {
	if !daemon.IsKnownPatrol(name) {
		return fmt.Errorf("unknown patrol %q\nKnown patrols: %s",
			name, strings.Join(daemon.KnownPatrols(), ", "))
	}

	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding town root: %w", err)
	}

	config := daemon.LoadPatrolConfig(townRoot)
	if config == nil {
		config = &daemon.DaemonPatrolConfig{
			Type:    "daemon-patrol-config",
			Version: 1,
		}
	}

	if err := daemon.SetPatrolEnabled(config, name, enabled); err != nil {
		return err
	}

	if err := daemon.SavePatrolConfig(townRoot, config); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	fmt.Printf("%s Patrol %q %s\n", style.Success.Render("✓"), name, action)
	fmt.Printf("  Restart the daemon for this to take effect on running patrols.\n")
	return nil
}

func runPatrolList(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return fmt.Errorf("finding town root: %w", err)
	}

	config := daemon.LoadPatrolConfig(townRoot)

	patrols := daemon.KnownPatrols()

	// Find max name length for alignment
	maxLen := 0
	for _, name := range patrols {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}

	fmt.Printf("Patrols (%s):\n\n", daemon.PatrolConfigFile(townRoot))
	for _, name := range patrols {
		enabled := daemon.IsPatrolEnabled(config, name)
		status := style.Success.Render("enabled")
		marker := "●"
		if !enabled {
			status = style.Dim.Render("disabled")
			marker = "○"
		}
		fmt.Printf("  %s %-*s  %s\n", marker, maxLen, name, status)
	}
	return nil
}
