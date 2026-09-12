package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/transcript"
	"andriiklymiuk/corgi/utils/agent/usage"

	"github.com/spf13/cobra"
)

// A session's conversation, on the phone: what you said, what Claude said,
// each tool call, each result — read from the transcript Claude Code
// writes, tailed. Off until this machine says which workspaces may be read
// (`corgi agent stream enable --workspace api`): a transcript carries code
// and, sometimes, a secret a tool printed, so the decision is made where
// the code lives, not on the phone. What leaves is scrubbed (transcript.Scrub),
// cut short, sealed like every body, and only ever pulled while a person
// is looking at that session's chat.

var agentStreamCmd = &cobra.Command{
	Use:   "stream",
	Short: "Which workspaces a paired phone may read as a conversation",
	Long: `A paired phone can show a session as a chat — your prompts, Claude's
words, every tool call and result — for sessions in the workspaces listed
here. Nothing streams until you enable a workspace, and nothing is pushed:
the phone pulls while its chat sheet is open, over the sealed link, and
the session's row shows an eye while it does. What looks like a secret in
a tool result is scrubbed before it leaves.

  corgi agent stream                          # what is allowed
  corgi agent stream enable --workspace api   # this workspace
  corgi agent stream enable --all             # every workspace
  corgi agent stream disable [--workspace api]`,
	Run: func(cmd *cobra.Command, _ []string) {
		user, err := config.LoadUser(agentUserConfigPath(mustAgentDir()))
		if err != nil {
			exitWithError("agent_stream", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"stream": user.Stream})
			return
		}
		switch {
		case len(user.Stream) == 0:
			fmt.Println("no workspace streams to the phone: corgi agent stream enable --workspace <id>")
		case user.StreamAllowed("*"):
			fmt.Println("every workspace's sessions may be read from the phone")
		default:
			fmt.Printf("readable from the phone: %s\n", strings.Join(user.Stream, ", "))
		}
	},
}

var agentStreamEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Let the phone read sessions in a workspace (--workspace), or all (--all)",
	Run: func(cmd *cobra.Command, _ []string) {
		workspace, _ := cmd.Flags().GetString("workspace")
		all, _ := cmd.Flags().GetBool("all")
		path := agentUserConfigPath(mustAgentDir())
		user, err := config.LoadUser(path)
		if err != nil {
			exitWithError("agent_stream", err, 1)
		}
		switch {
		case all:
			user.Stream = []string{"*"}
		case workspace != "":
			if _, err := workspaceRoot(workspace); err != nil {
				exitWithError("agent_stream", err, 2)
			}
			if !containsString(user.Stream, workspace) && !containsString(user.Stream, "*") {
				user.Stream = append(user.Stream, workspace)
				sort.Strings(user.Stream)
			}
		default:
			if _, id := workspaceRootFor(mustCwd()); id != "" {
				if !containsString(user.Stream, id) && !containsString(user.Stream, "*") {
					user.Stream = append(user.Stream, id)
					sort.Strings(user.Stream)
				}
			} else {
				exitWithError("agent_stream", fmt.Errorf("--workspace <id> or --all"), 2)
			}
		}
		if err := writeUserConfig(path, user); err != nil {
			exitWithError("agent_stream", err, 1)
		}
		utils.Infof("✓ readable from the phone: %s\n", strings.Join(user.Stream, ", "))
	},
}

var agentStreamDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Stop the phone reading sessions in a workspace (--workspace), or everywhere",
	Run: func(cmd *cobra.Command, _ []string) {
		workspace, _ := cmd.Flags().GetString("workspace")
		path := agentUserConfigPath(mustAgentDir())
		user, err := config.LoadUser(path)
		if err != nil {
			exitWithError("agent_stream", err, 1)
		}
		if workspace == "" {
			user.Stream = nil
		} else {
			kept := user.Stream[:0]
			for _, w := range user.Stream {
				if w != workspace {
					kept = append(kept, w)
				}
			}
			user.Stream = kept
		}
		if err := writeUserConfig(path, user); err != nil {
			exitWithError("agent_stream", err, 1)
		}
		if len(user.Stream) == 0 {
			utils.Info("✓ nothing streams to the phone")
			return
		}
		utils.Infof("✓ readable from the phone: %s\n", strings.Join(user.Stream, ", "))
	},
}

// streamAllowedFor is the seam the handler asks: may this session's
// workspace be read?
var streamAllowedFor = func(workspace string) bool {
	dir, err := agentDir()
	if err != nil {
		return false
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil {
		return false
	}
	return user.StreamAllowed(workspace)
}

// transcriptPathFor is the seam: where a session's conversation is.
var transcriptPathFor = func(s sessions.Session) string {
	if sessions.Placeholder(s.ID) {
		return ""
	}
	// The folder is named after where the session started; a session that
	// has cd'd since still writes to the file it began with.
	home := s.Home
	if home == "" {
		home = s.Cwd
	}
	if home != "" {
		if p := usage.TranscriptPath(s.ConfigDir, home, s.ID); transcript.Exists(p) {
			return p
		}
	}
	// A session the daemon met after it had moved: the id is unique, find
	// it under whichever project folder Claude Code filed it.
	base := s.ConfigDir
	if base == "" {
		if h, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(h, ".claude")
		}
	}
	if base == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(base, "projects", "*", s.ID+".jsonl"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// maxStreamWait bounds the long-poll: the phone asks again after.
const maxStreamWait = 25 * time.Second

// launchTranscriptHandler is the phone's chat: POST {session, after, wait}.
// after 0 (or absent) opens the conversation at its newest entries; a
// later after continues from where the answer said. With wait, the answer
// holds until the transcript grows or the seconds run out — one request in
// flight per open chat, a line on the phone within a second of the laptop.
func launchTranscriptHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {session, after, wait} to read a conversation")
		return
	}
	var req struct {
		Session string `json:"session"`
		After   int64  `json:"after"`
		Wait    int    `json:"wait"`
		Max     int    `json:"max"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, "could not read the request")
		return
	}
	session, code, msg := launchSessionFor(req.Session)
	if code != 0 {
		writeLaunchError(w, code, msg)
		return
	}
	if !streamAllowedFor(session.Label) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reading sessions of %s from a phone is off on the laptop: corgi agent stream enable --workspace %s", session.Label, session.Label))
		return
	}
	path := transcriptPathFor(session)
	if path == "" || !transcript.Exists(path) {
		writeLaunchJSON(w, map[string]any{"entries": []transcript.Entry{}, "offset": 0, "empty": true})
		return
	}
	// The row shows an eye: the laptop always knows a phone is reading.
	if dir, err := agentDir(); err == nil {
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil && info.Commands {
			if _, err := command.Write(dir, command.Command{Action: command.ActionRead, SessionID: session.ID, Source: "phone"}); err == nil {
				daemon.Nudge(info)
			}
		}
	}
	if req.After <= 0 {
		entries, offset, err := transcript.Last(path, req.Max)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not read the conversation")
			return
		}
		if entries == nil {
			entries = []transcript.Entry{}
		}
		writeLaunchJSON(w, map[string]any{"entries": entries, "offset": offset})
		return
	}
	wait := time.Duration(req.Wait) * time.Second
	if wait > maxStreamWait {
		wait = maxStreamWait
	}
	deadline := time.Now().Add(wait)
	for {
		entries, offset, err := transcript.Read(path, req.After, req.Max)
		if err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not read the conversation")
			return
		}
		if len(entries) > 0 || offset != req.After || wait == 0 || time.Now().After(deadline) {
			if entries == nil {
				entries = []transcript.Entry{}
			}
			writeLaunchJSON(w, map[string]any{"entries": entries, "offset": offset})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func init() {
	agentStreamEnableCmd.Flags().String("workspace", "", "The registered workspace (default: the one you are in)")
	agentStreamEnableCmd.Flags().Bool("all", false, "Every workspace")
	agentStreamDisableCmd.Flags().String("workspace", "", "Only this workspace (default: everywhere)")
	agentStreamCmd.AddCommand(agentStreamEnableCmd, agentStreamDisableCmd)
	agentCmd.AddCommand(agentStreamCmd)
}
