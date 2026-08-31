package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type stackEvent struct {
	At       string `json:"at"`
	Service  string `json:"service"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Port     int    `json:"port,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
}

var (
	eventsFollow   bool
	eventsInterval time.Duration
	eventsTimeout  time.Duration
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Service lifecycle as a stream, instead of polling status",
	Long: `Reports what the stack is doing: a service starting, going ready, crashing or
being stopped. Without --follow it prints the current state once and exits.

With --follow it stays open and emits a line per transition, so a script or an
agent reacts to a crash instead of polling corgi status on a timer.`,
	Example: `corgi events --json

corgi events --follow --json --timeout 10m`,
	Run: runEvents,
}

func init() {
	rootCmd.AddCommand(eventsCmd)
	eventsCmd.Flags().BoolVar(&eventsFollow, "follow", false, "Stay open and print each transition as it happens")
	eventsCmd.Flags().DurationVar(&eventsInterval, "interval", time.Second, "How often to re-read the run state while following")
	eventsCmd.Flags().DurationVar(&eventsTimeout, "timeout", 0, "Stop following after this long (0 = until Ctrl-C)")
}

func runEvents(cmd *cobra.Command, _ []string) {
	mustLoadCorgiServices(cmd)

	statuses, ports, exits := readStackState()
	if len(statuses) == 0 && !eventsFollow {
		utils.Info("no run state yet — start the stack with corgi run --detach")
		return
	}
	for _, event := range baselineEvents(statuses, ports, exits) {
		emitStackEvent(event)
	}
	if !eventsFollow {
		return
	}

	deadline := time.Time{}
	if eventsTimeout > 0 {
		deadline = time.Now().Add(eventsTimeout)
	}
	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return
		}
		time.Sleep(eventsInterval)
		next, nextPorts, nextExits := readStackState()
		for _, event := range diffStackState(statuses, next, nextPorts, nextExits) {
			emitStackEvent(event)
		}
		statuses = next
	}
}

func readStackState() (statuses map[string]string, ports map[string]int, exits map[string]*int) {
	statuses, ports, exits = map[string]string{}, map[string]int{}, map[string]*int{}
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err != nil {
		return statuses, ports, exits
	}
	state, err := utils.ReadRunState(statePath)
	if err != nil {
		return statuses, ports, exits
	}
	reconciled := utils.ReconcileRunState(state, utils.PidAlive, utils.ContainerRunning)
	for _, entry := range append(append([]utils.RunStateEntry{}, reconciled.Services...), reconciled.DBServices...) {
		statuses[entry.Name] = entry.Status
		ports[entry.Name] = entry.Port
		exits[entry.Name] = entry.ExitCode
	}
	return statuses, ports, exits
}

func baselineEvents(statuses map[string]string, ports map[string]int, exits map[string]*int) []stackEvent {
	names := sortedNames(statuses)
	events := make([]stackEvent, 0, len(names))
	for _, name := range names {
		events = append(events, stackEvent{
			At: nowStamp(), Service: name, Kind: "state",
			Status: statuses[name], Port: ports[name], ExitCode: exits[name],
		})
	}
	return events
}

func diffStackState(previous, next map[string]string, ports map[string]int, exits map[string]*int) []stackEvent {
	var events []stackEvent
	for _, name := range sortedNames(next) {
		if previous[name] == next[name] {
			continue
		}
		events = append(events, stackEvent{
			At: nowStamp(), Service: name, Kind: transitionKind(next[name]),
			Status: next[name], Port: ports[name], ExitCode: exits[name],
		})
	}
	for _, name := range sortedNames(previous) {
		if _, still := next[name]; !still {
			events = append(events, stackEvent{At: nowStamp(), Service: name, Kind: "gone", Status: "gone"})
		}
	}
	return events
}

func transitionKind(status string) string {
	switch status {
	case "running":
		return "started"
	case "crashed":
		return "crashed"
	case "stopped":
		return "stopped"
	default:
		return "state"
	}
}

func sortedNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

func emitStackEvent(event stackEvent) {
	if utils.JSONOutput {
		fmt.Println(compactStackEvent(event))
		return
	}
	line := fmt.Sprintf("%s  %-16s %s", event.At, event.Service, event.Kind)
	if event.Status != "" && event.Status != event.Kind {
		line += " · " + event.Status
	}
	if event.ExitCode != nil {
		line += fmt.Sprintf(" · exit %d", *event.ExitCode)
	}
	utils.Info(line)
}

func compactStackEvent(event stackEvent) string {
	data, err := json.Marshal(event)
	if err != nil {
		return "{}"
	}
	return string(data)
}
