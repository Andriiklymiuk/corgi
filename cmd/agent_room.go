package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/rooms"
	"github.com/spf13/cobra"
)

var agentRoomCmd = &cobra.Command{
	Use:   "room",
	Short: "Rooms where agents and people leave each other messages: post, read, reply in a thread, with checked data and per-device rights",
	Long: `A room is a named list of messages on this machine, for agents that do not
share a session: a finding one agent leaves for another, a question a person
answers later, the running notes of a long job. Names read like paths.

  corgi agent room                                       the rooms
  corgi agent room post security/findings "SQL built from input in api/search.go"
  corgi agent room read security/findings                the last messages
  corgi agent room read security/findings --after m12 --wait 10m     what came since, or wait for it
  corgi agent room post security/findings "fixed in #88" --thread m12
  corgi agent room read security/findings --thread m12   one thread: its first message and the replies

Data with a shape is checked before it is kept:

  corgi agent room shape add finding@1 --file finding.json --about "one security finding"
  corgi agent room post security/findings --shape finding@1 --data '{"repo":"acme/api","severity":"high"}'
  corgi agent room read security/findings --label kind=bug

A shape is a small JSON Schema: type, required, properties, items, enum,
maxLength, minimum, maximum. A saved shape never changes; a change is the next
version (finding@2).

On this machine every command may read and post. A paired device reaches rooms
over /launch/rooms. One with full control (your phone, an agent you paired)
reads and posts everywhere until a grant names it or everyone; from then on
its grants decide. A read-only device needs a grant. Grants go by path:

  corgi agent room grant ci-runner security/=read,post
  corgi agent room grant everyone all=read
  corgi agent room grant ci-runner security/secrets=none
  corgi agent room can ci-runner security/findings post   why, or why not
  corgi agent room grants
  corgi agent room revoke g2

The longest path wins, and a grant by name wins over one for everyone.

A message can name a live session, and that session gets it as its next
prompt, with the command to answer in the room:

  corgi agent room                                       also lists who can be named
  corgi agent room post plans/search "@api what does the index cost? @web can you page it?"
  corgi agent room post plans/search "@here one more round"     everyone who wrote in this room

Every workspace and every ticket with a live session has its own room, and a
message there needs no name: it goes to the sessions on it.

  corgi agent room post ws/api "is the index migration safe to run twice?"
  corgi agent room post ticket/abc-123 "what is left?"

A name is the session's label in lower case with dashes. A session that waits
on a permission or sits at a limit is not written to, and the answer says so.
A room carries up to 30 such messages in ten minutes, so two agents cannot
call each other without end. A session that posts is named by its own label;
its reply to a thread a phone started, or one with @you, rings the phone.`,
	Args: cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		dir := mustAgentDir()
		list, agents := rooms.List(dir), roomAgents(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"rooms": list, "agents": agents, "places": roomPlaces(agents, list), "me": peers.Me()})
			return
		}
		if len(list) == 0 {
			fmt.Println("no rooms yet - corgi agent room post <room> \"words\" opens one")
		}
		for _, r := range list {
			fmt.Printf("%-32s %4d  %s\n", r.Name, r.Messages, strings.TrimSpace(roomAgo(r.Last)+" "+r.LastBy))
		}
		for i, p := range roomPlaces(agents, list) {
			if i == 0 {
				fmt.Println("\nworkspaces and tickets (a message there goes to the sessions on it):")
			}
			fmt.Printf("  %-32s %s\n", p.Room, strings.Join(p.Agents, ", "))
		}
		for i, a := range agents {
			if i == 0 {
				fmt.Println("\ncan be named:")
			}
			where := strings.Join(reviewFacts(a.Workspace, a.Branch, a.Ticket), " · ")
			fmt.Printf("  @%-30s %-28s %s\n", a.Handle, where, strings.TrimSpace(a.Status+" "+a.Busy))
		}
	},
}

func roomAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return time.Since(t).Round(time.Minute).String() + " ago"
}

func roomFail(err error) {
	if err != nil {
		exitWithError("agent_room", err, 2)
	}
}

func messageLine(m rooms.Message) string {
	var b strings.Builder
	indent := ""
	if m.Thread != "" {
		indent = "    "
	}
	fmt.Fprintf(&b, "%s%-5s %s  %s\n", indent, m.ID, m.By, m.At.Local().Format("Jan 2 15:04"))
	if m.Text != "" {
		for _, line := range strings.Split(m.Text, "\n") {
			fmt.Fprintf(&b, "%s      %s\n", indent, line)
		}
	}
	if len(m.Data) > 0 {
		fmt.Fprintf(&b, "%s      %s %s\n", indent, m.Shape, string(m.Data))
	}
	return b.String()
}

var agentRoomPostCmd = &cobra.Command{
	Use:   "post <room> [words]",
	Short: "Leave a message in a room, or a reply in a thread",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		who, _ := cmd.Flags().GetString("as")
		thread, _ := cmd.Flags().GetString("thread")
		shape, _ := cmd.Flags().GetString("shape")
		data, _ := cmd.Flags().GetString("data")
		pairs, _ := cmd.Flags().GetStringArray("label")
		labels, err := rooms.ParseLabels(pairs)
		roomFail(err)
		by, via := strings.TrimSpace(who), ""
		if by == "" {
			if handle, ok := roomAuthor(dir); ok {
				by, via = handle, roomViaAgent
			}
		}
		out, err := postToRoom(dir, rooms.Message{
			Room: args[0], Thread: thread, By: firstNonEmpty(by, peers.Me()), Via: via,
			Text: strings.Join(args[1:], " "), Shape: shape, Data: json.RawMessage(strings.TrimSpace(data)), Labels: labels,
		}, true, time.Now())
		roomFail(err)
		m := out.Message
		if utils.JSONOutput {
			utils.PrintJSON(out)
			return
		}
		for _, handle := range out.Sent {
			fmt.Printf("sent to @%s\n", handle)
		}
		for handle, why := range out.Missed {
			fmt.Printf("@%s did not get it: %s\n", handle, why)
		}
		fmt.Printf("%s in %s\n", m.ID, m.Room)
	},
}

var agentRoomReadCmd = &cobra.Command{
	Use:   "read <room>",
	Short: "Read a room: the last messages, what came after one, one thread, or wait for the next",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		after, _ := cmd.Flags().GetString("after")
		thread, _ := cmd.Flags().GetString("thread")
		limit, _ := cmd.Flags().GetInt("max")
		wait, _ := cmd.Flags().GetDuration("wait")
		pairs, _ := cmd.Flags().GetStringArray("label")
		labels, err := rooms.ParseLabels(pairs)
		roomFail(err)
		q := rooms.Query{Room: args[0], After: after, Thread: thread, Label: labels, Limit: limit}
		deadline := time.Now().Add(wait)
		list, err := rooms.Read(dir, q)
		for err == nil && len(list) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Second)
			list, err = rooms.Read(dir, q)
		}
		roomFail(err)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"room": args[0], "messages": list})
			return
		}
		if len(list) == 0 {
			fmt.Println("nothing new")
			return
		}
		for _, m := range list {
			fmt.Print(messageLine(m))
		}
	},
}

var agentRoomShapeCmd = &cobra.Command{
	Use:   "shape [name@version]",
	Short: "The shapes data can be checked against; with a name, that shape",
	Args:  cobra.MaximumNArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		dir := mustAgentDir()
		if len(args) == 1 {
			s, err := rooms.LoadShape(dir, args[0])
			roomFail(err)
			utils.PrintJSON(s)
			return
		}
		list := rooms.Shapes(dir)
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"shapes": list})
			return
		}
		if len(list) == 0 {
			fmt.Println("no shapes - corgi agent room shape add finding@1 --file finding.json")
			return
		}
		for _, s := range list {
			fmt.Printf("%-28s %s\n", s.Name, s.About)
		}
	},
}

var agentRoomShapeAddCmd = &cobra.Command{
	Use:   "add <name@version>",
	Short: "Save a shape from a JSON Schema file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		file, _ := cmd.Flags().GetString("file")
		about, _ := cmd.Flags().GetString("about")
		raw, err := os.ReadFile(file)
		roomFail(err)
		s, err := rooms.SaveShape(mustAgentDir(), args[0], raw, about)
		roomFail(err)
		if utils.JSONOutput {
			utils.PrintJSON(s)
			return
		}
		fmt.Printf("saved %s\n", s.Name)
	},
}

var agentRoomGrantCmd = &cobra.Command{
	Use:   "grant <device|everyone> <path=rights>",
	Short: "Let a paired device read or post under a path: security/=read,post, all=read, security/secrets=none",
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		g, err := rooms.AddGrant(mustAgentDir(), args[0], args[1], time.Now())
		roomFail(err)
		if utils.JSONOutput {
			utils.PrintJSON(g)
			return
		}
		fmt.Printf("%s: %s\n", g.ID, grantLine(g))
	},
}

func grantLine(g rooms.Grant) string {
	path := "every room"
	if g.Path != "" {
		path = g.Path + "/"
	}
	rights := strings.Join(g.Can, ", ")
	if rights == "" {
		rights = "nothing"
	}
	return fmt.Sprintf("%s may %s in %s", g.Who, rights, path)
}

var agentRoomGrantsCmd = &cobra.Command{
	Use:   "grants",
	Short: "Every grant, with its id",
	Args:  cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		list := rooms.Grants(mustAgentDir())
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"grants": list})
			return
		}
		if len(list) == 0 {
			fmt.Println("no grants: paired devices reach no room")
			return
		}
		for _, g := range list {
			fmt.Printf("%-4s %s\n", g.ID, grantLine(g))
		}
	},
}

var agentRoomRevokeCmd = &cobra.Command{
	Use:   "revoke <id>",
	Short: "Take a grant back",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		g, err := rooms.RevokeGrant(mustAgentDir(), args[0])
		roomFail(err)
		fmt.Printf("revoked %s: %s\n", g.ID, grantLine(g))
	},
}

var agentRoomCanCmd = &cobra.Command{
	Use:   "can <device> <room> [read|post]",
	Short: "Whether a device may read or post in a room, and which grant says so",
	Args:  cobra.RangeArgs(2, 3),
	Run: func(_ *cobra.Command, args []string) {
		room, err := rooms.CleanName(args[1])
		roomFail(err)
		action := rooms.CanRead
		if len(args) == 3 {
			action = strings.ToLower(args[2])
		}
		v := rooms.Explain(rooms.Grants(mustAgentDir()), args[0], room, action)
		if utils.JSONOutput {
			utils.PrintJSON(v)
			return
		}
		fmt.Printf("%s: %s\n", map[bool]string{true: "yes", false: "no"}[v.Allowed], v.Because)
	},
}

func init() {
	agentRoomPostCmd.Flags().String("as", "", "Who writes it (default: this machine's name)")
	agentRoomPostCmd.Flags().String("thread", "", "Reply under this message id")
	agentRoomPostCmd.Flags().String("shape", "", "Check --data against this shape, name@version")
	agentRoomPostCmd.Flags().String("data", "", "JSON to keep with the message")
	agentRoomPostCmd.Flags().StringArray("label", nil, "key=value, repeatable: kind=bug")
	agentRoomReadCmd.Flags().String("after", "", "Only messages after this id")
	agentRoomReadCmd.Flags().String("thread", "", "Only this thread")
	agentRoomReadCmd.Flags().StringArray("label", nil, "Only messages with key=value, repeatable")
	agentRoomReadCmd.Flags().Int("max", 50, "At most this many")
	agentRoomReadCmd.Flags().Duration("wait", 0, "With nothing to show, wait this long for a message: 10m")
	agentRoomShapeAddCmd.Flags().String("file", "", "The JSON Schema file")
	agentRoomShapeAddCmd.Flags().String("about", "", "One line on what it is for")
	_ = agentRoomShapeAddCmd.MarkFlagRequired("file")
	agentRoomShapeCmd.AddCommand(agentRoomShapeAddCmd)
	agentRoomCmd.AddCommand(agentRoomPostCmd, agentRoomReadCmd, agentRoomShapeCmd, agentRoomGrantCmd, agentRoomGrantsCmd, agentRoomRevokeCmd, agentRoomCanCmd)
	agentCmd.AddCommand(agentRoomCmd)
}
