package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/atomicfile"
	"github.com/spf13/cobra"
)

const (
	sealedHeader    = "X-Corgi-Sealed"
	phoneGrantLife  = 30 * 24 * time.Hour
	phoneGitHubAPI  = "https://api.github.com"
	phoneGitLabHost = "https://gitlab.com"
)

type phoneReviewsDevice struct {
	Since time.Time `json:"since"`
	Given time.Time `json:"given,omitzero"`
}

type phoneReviews struct {
	Devices map[string]phoneReviewsDevice `json:"devices"`
	Refused map[string]time.Time          `json:"refused,omitempty"`
	Locked  bool                          `json:"locked,omitempty"`
}

func (p phoneReviews) refuses(device string) bool {
	_, refused := p.Refused[device]
	return p.Locked || refused
}

func phoneReviewsPath(dir string) string { return filepath.Join(dir, "reviews", "phone.json") }

func loadPhoneReviews(dir string) phoneReviews {
	out := phoneReviews{Devices: map[string]phoneReviewsDevice{}}
	raw, err := os.ReadFile(phoneReviewsPath(dir))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out.Devices == nil {
		out.Devices = map[string]phoneReviewsDevice{}
	}
	return out
}

func savePhoneReviews(dir string, p phoneReviews) error {
	path := phoneReviewsPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0o600)
}

func phoneReviewsAllowed(dir, device string) bool {
	if device == "" {
		return false
	}
	_, ok := loadPhoneReviews(dir).Devices[device]
	return ok
}

func phoneReviewsCandidates(dir string) []pairing.Device {
	store, err := pairing.Load(pairing.StorePath(dir))
	if err != nil {
		return nil
	}
	var out []pairing.Device
	now := time.Now()
	for _, d := range store.Devices {
		if d.Viewer() || d.Peer() || !d.Encrypted() || d.Expired(now) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type phoneForge struct {
	GitHub    string       `json:"github,omitempty"`
	GitHubAPI string       `json:"githubApi,omitempty"`
	GitLab    string       `json:"gitlab,omitempty"`
	GitLabURL string       `json:"gitlabUrl,omitempty"`
	Seats     []phoneSeats `json:"seats"`
}

type phoneSeats struct {
	Workspace string   `json:"workspace"`
	Repos     []string `json:"repos"`
}

func httpsHost(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", false
	}
	return "https://" + u.Host, true
}

var phoneForgeSecrets = func(dir, ws string) watch.Secrets { return watch.NewForge(watch.LoadSecretsFor(dir, ws)).Secrets }

func phoneForges(dir string) []phoneForge {
	var out []phoneForge
	for _, seats := range reviewSeats(dir) {
		s := phoneForgeSecrets(dir, seats[0].id)
		f := phoneForge{Seats: []phoneSeats{}}
		if s.GitHub != "" {
			f.GitHub, f.GitHubAPI = s.GitHub, phoneGitHubAPI
		}
		if s.GitLab != "" {
			host, ok := httpsHost(firstNonEmpty(s.GitLabURL, phoneGitLabHost))
			if ok {
				f.GitLab, f.GitLabURL = s.GitLab, host
			}
		}
		if f.GitHub == "" && f.GitLab == "" {
			continue
		}
		for _, seat := range seats {
			repos := seat.repos
			if repos == nil {
				repos = []string{}
			}
			f.Seats = append(f.Seats, phoneSeats{Workspace: seat.id, Repos: repos})
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return firstSeat(out[i]) < firstSeat(out[j]) })
	return out
}

func firstSeat(f phoneForge) string {
	if len(f.Seats) == 0 {
		return ""
	}
	return f.Seats[0].Workspace
}

func launchReviewsGrantHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	device := deviceOf(r)
	switch {
	case device == "":
		writeLaunchError(w, http.StatusForbidden, "only a paired phone may ask for this")
		return
	case isViewer(r):
		writeLaunchError(w, http.StatusForbidden, "a read-only device does not review without the Mac")
		return
	case r.Header.Get(sealedHeader) != "1":
		writeLaunchError(w, http.StatusForbidden, "this device did not pair with a key; pair again, then ask")
		return
	}
	list := loadPhoneReviews(dir)
	entry, ok := list.Devices[device]
	asks := r.Method == http.MethodPost
	if !ok && (!asks || list.refuses(device)) {
		writeLaunchError(w, http.StatusForbidden, fmt.Sprintf("reviews without the Mac are off for %s. On the Mac: corgi agent reviews phone on %q", device, device))
		return
	}
	if !ok {
		entry = phoneReviewsDevice{Since: time.Now()}
		phoneTookReviews(device)
	}
	forges := phoneForges(dir)
	if len(forges) == 0 {
		writeLaunchError(w, http.StatusNotFound, "this Mac holds no GitHub or GitLab token to review with")
		return
	}
	now := time.Now()
	entry.Given = now
	list.Devices[device] = entry
	_ = savePhoneReviews(dir, list)
	utils.Infof("reviews: gave %s the review access for the next %d days\n", device, int(phoneGrantLife.Hours()/24))
	writeLaunchJSON(w, map[string]any{"forges": forges, "until": now.Add(phoneGrantLife), "device": device})
}

var agentReviewsPhoneCmd = &cobra.Command{
	Use:   "phone [on|off] [device]",
	Short: "Let a paired phone review while this Mac is off: it gets the Mac's GitHub and GitLab access, sealed, to keep in its Keychain",
	Long: `With the Mac asleep, a phone cannot ask it for pull requests. Turned on for
a phone, that phone asks the Mac once for the GitHub and GitLab tokens the
Mac reviews with and for the list of repos per workspace, and from then on
reads and answers pull requests itself when the Mac does not answer.

  corgi agent reviews phone                     who has it, and when each last took it
  corgi agent reviews phone on "Anna's iPhone"
  corgi agent reviews phone off "Anna's iPhone"
  corgi agent reviews phone off                 everyone, and no phone may ask again

A paired phone with full control can also turn it on itself, from its
Reviews tab: this Mac shows a notification when one does.

What keeps it safe:
  - off until the phone's owner turns it on, on the phone behind Face ID or
    here by name; off here stays off, the phone cannot turn it back on
  - only a phone with full control that paired with a key; a read-only
    device, a peer laptop and the launcher token are refused
  - the answer travels sealed with the pairing key, never in the clear
  - a GitLab on plain http is left out
  - the phone keeps it in its Keychain, on that phone only, behind Face ID,
    and sends it only to that GitHub or GitLab
  - it lasts 30 days, then the phone asks again; off here, and the phone
    drops it the next time it hears from this Mac; unpair it and it is gone

The token is the same one this Mac uses, with all it may do. A lost phone:
turn it off here and rotate the token (gh auth refresh, or a new GitLab token).`,
	Args: cobra.MaximumNArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		dir := mustAgentDir()
		list := loadPhoneReviews(dir)
		candidates := phoneReviewsCandidates(dir)
		if len(args) == 0 {
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"devices": list.Devices, "candidates": deviceNames(candidates)})
				return
			}
			if len(candidates) == 0 {
				fmt.Println("no phone with full control is paired with a key")
				return
			}
			for _, d := range candidates {
				entry, on := list.Devices[d.Name]
				state := "off"
				if on {
					state = "on"
					if !entry.Given.IsZero() {
						state += ", last taken " + entry.Given.Local().Format("Jan 2 15:04")
					}
				}
				fmt.Printf("%-32s %s\n", d.Name, state)
			}
			return
		}
		turn := strings.ToLower(args[0])
		if turn != "on" && turn != "off" {
			exitWithError("agent_reviews_phone", fmt.Errorf("say on or off, not %q", args[0]), 2)
		}
		name := ""
		if len(args) == 2 {
			name = strings.TrimSpace(args[1])
		}
		if turn == "off" {
			if name == "" {
				list.Devices, list.Locked = map[string]phoneReviewsDevice{}, true
			} else {
				delete(list.Devices, name)
				if list.Refused == nil {
					list.Refused = map[string]time.Time{}
				}
				list.Refused[name] = time.Now()
			}
			if err := savePhoneReviews(dir, list); err != nil {
				exitWithError("agent_reviews_phone", err, 1)
			}
			fmt.Println("off. The phone drops what it holds the next time it hears from this Mac. For a lost phone, rotate the tokens too.")
			return
		}
		if name == "" && len(candidates) == 1 {
			name = candidates[0].Name
		}
		found := false
		for _, d := range candidates {
			if d.Name == name {
				found = true
			}
		}
		if !found && len(candidates) == 0 {
			exitWithError("agent_reviews_phone", fmt.Errorf("no phone with full control is paired with a key: pair one first (corgi agent pair)"), 2)
		}
		if !found {
			exitWithError("agent_reviews_phone", fmt.Errorf("name one of: %s", strings.Join(deviceNames(candidates), ", ")), 2)
		}
		if _, ok := list.Devices[name]; !ok {
			list.Devices[name] = phoneReviewsDevice{Since: time.Now()}
		}
		list.Locked = false
		delete(list.Refused, name)
		if err := savePhoneReviews(dir, list); err != nil {
			exitWithError("agent_reviews_phone", err, 1)
		}
		fmt.Printf("on for %s. It takes the access the next time it opens Reviews with this Mac in reach.\n", name)
	},
}

var phoneTookReviews = func(device string) {
	utils.NotifyRaw("corgi", fmt.Sprintf("%s now reviews pull requests without this Mac. To stop it: corgi agent reviews phone off %q", device, device))
}

func deviceNames(list []pairing.Device) []string {
	out := []string{}
	for _, d := range list {
		out = append(out, d.Name)
	}
	return out
}

func init() {
	agentReviewsCmd.AddCommand(agentReviewsPhoneCmd)
}
