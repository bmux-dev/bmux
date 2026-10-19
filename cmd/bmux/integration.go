package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/josephferrero/bmux/internal/integration"
)

const integrationUsage = "usage: bmux integration status [AGENT] | install [AGENT...] | uninstall AGENT..."

// runIntegration installs, removes and lists agent integrations. The hidden
// `hook AGENT` form is what installed hook scripts run, with the agent's
// event on stdin; it always succeeds quietly so it can never disturb the agent.
func runIntegration(socket string, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 2 && args[0] == "hook" {
		if report, ok := integration.HookReport(args[1], stdin); ok {
			_ = sendAgentReport(socket, report.State, report.Label, report.Resume)
		}
		return nil
	}
	env, err := integration.System()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("%s", integrationUsage)
	}
	names := args[1:]
	targets, err := integrationTargets(names)
	if err != nil {
		return err
	}
	switch args[0] {
	case "status":
		if len(names) > 1 {
			return fmt.Errorf("%s", integrationUsage)
		}
		w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "AGENT\tSTATUS\tCONFIG")
		for _, a := range targets {
			status, err := a.Status(env)
			state := status.String()
			if err != nil {
				state += " (" + err.Error() + ")"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", a.Name, state, a.ConfigDir(env))
		}
		return w.Flush()
	case "install":
		if len(names) == 0 {
			// Like the integrations panel: everything on PATH or out of date.
			var pending []integration.Agent
			for _, a := range targets {
				if status, _ := a.Status(env); status == integration.Available || status == integration.Outdated {
					pending = append(pending, a)
				}
			}
			if len(pending) == 0 {
				fmt.Fprintln(stdout, "No integrations to install or update; see bmux integration status")
				return nil
			}
			targets = pending
		}
		var failed []string
		done := 0
		for _, a := range targets {
			if err := a.Install(env); err != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", a.Name, err))
				continue
			}
			done++
			fmt.Fprintf(stdout, "Installed the %s integration in %s\n", a.Label, a.ConfigDir(env))
			if a.Notes != "" {
				fmt.Fprintln(stdout, "  "+a.Notes)
			}
		}
		if done > 0 {
			fmt.Fprintln(stdout, "Restart running agents to pick up new integrations.")
		}
		if len(failed) > 0 {
			return fmt.Errorf("%s", strings.Join(failed, "; "))
		}
		return nil
	case "uninstall":
		if len(names) == 0 {
			return fmt.Errorf("%s", integrationUsage)
		}
		for _, a := range targets {
			if err := a.Uninstall(env); err != nil {
				return fmt.Errorf("%s: %w", a.Name, err)
			}
			fmt.Fprintf(stdout, "Removed the %s integration from %s\n", a.Label, a.ConfigDir(env))
		}
		return nil
	}
	return fmt.Errorf("%s", integrationUsage)
}

// integrationTargets resolves agent names, or every agent when none are given.
func integrationTargets(names []string) ([]integration.Agent, error) {
	if len(names) == 0 {
		return integration.All(), nil
	}
	var result []integration.Agent
	for _, name := range names {
		a, ok := integration.Find(name)
		if !ok {
			var known []string
			for _, a := range integration.All() {
				known = append(known, a.Name)
			}
			return nil, fmt.Errorf("unknown agent %q; integrations exist for %s", name, strings.Join(known, ", "))
		}
		result = append(result, a)
	}
	return result, nil
}
