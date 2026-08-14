package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"

	"github.com/spf13/cobra"
)

// Pairing exists so a phone never has to be handed the server's own bearer
// token. That token reaches corgi_exec and corgi_db_query, so a QR containing
// it is a credential for the machine — visible to anyone who sees the screen,
// and impossible to revoke for one device without re-pairing every other.

// pairRequest is what a client posts to /pair.
type pairRequest struct {
	Code   string `json:"code"`
	Device string `json:"device"`
}

type pairResponse struct {
	Token   string `json:"token"`
	Daemon  string `json:"daemon"`
	Device  string `json:"device"`
	Version string `json:"version"`
}

// maxPairBodyBytes bounds the request body. The payload is two short strings;
// anything larger is a mistake or an attempt to make the server allocate.
const maxPairBodyBytes = 4 << 10

// pairingHandler serves POST /pair while a pairing window is open.
func pairingHandler(session *pairing.Session, storePath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", mimeJSON)

		if r.Method != http.MethodPost {
			writePairError(w, http.StatusMethodNotAllowed, "POST a {code, device} body to pair")
			return
		}
		if !session.Open() {
			// Deliberately vague about why: an expired window and a used one
			// are the same answer to anyone who should not be here.
			writePairError(w, http.StatusForbidden, "pairing is not open — run `corgi mcp --http --pair` on the machine")
			return
		}

		var req pairRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPairBodyBytes)).Decode(&req); err != nil {
			writePairError(w, http.StatusBadRequest, "could not read the pairing request")
			return
		}

		token, err := pairing.Pair(storePath, session, req.Code, req.Device)
		if err != nil {
			// Errors here are about the code or the name, both supplied by the
			// caller, so they are safe to return verbatim and useful to a person.
			writePairError(w, http.StatusForbidden, err.Error())
			return
		}

		host, _ := os.Hostname()
		utils.Infof("paired device %q\n", strings.TrimSpace(req.Device))
		_ = json.NewEncoder(w).Encode(pairResponse{
			Token:   token,
			Daemon:  host,
			Device:  strings.TrimSpace(req.Device),
			Version: APP_VERSION,
		})
	})
}

func writePairError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// announcePairing prints the code and how to use it. The code is short-lived
// and single-use, which is the whole reason it is safe to display.
func announcePairing(code, addr string) {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "  pairing code: %s\n", code)
	fmt.Fprintf(os.Stderr, "  valid for %s, single use\n", pairing.CodeTTL)
	fmt.Fprintf(os.Stderr, "  POST http://%s/pair  {\"code\":\"%s\",\"device\":\"my-phone\"}\n", localURL(addr), code)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Paired devices get their own token, revocable with `corgi mcp devices revoke <name>`.")
	fmt.Fprintln(os.Stderr, "")
}

// ---------------------------------------------------------------- devices CLI

var mcpDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List and revoke devices paired with corgi mcp",
	Run:   runMCPDevicesList,
}

var mcpDevicesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List paired devices",
	Run:   runMCPDevicesList,
}

var mcpDevicesRevokeCmd = &cobra.Command{
	Use:   "revoke <name>",
	Short: "Revoke one device's access, leaving the others working",
	Args:  cobra.ExactArgs(1),
	Run:   runMCPDevicesRevoke,
}

func mcpDeviceStorePath() string {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	return pairing.StorePath(dir)
}

func runMCPDevicesList(_ *cobra.Command, _ []string) {
	store, err := pairing.Load(mcpDeviceStorePath())
	if err != nil {
		exitWithError("mcp_devices_read", err, 1)
	}

	if utils.JSONOutput {
		// Hashes are omitted: they are not needed to answer "which devices are
		// paired", and printing them invites treating them as identifiers.
		type row struct {
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"createdAt"`
		}
		rows := make([]row, 0, len(store.Devices))
		for _, d := range store.Devices {
			rows = append(rows, row{Name: d.Name, CreatedAt: d.CreatedAt})
		}
		utils.PrintJSON(rows)
		return
	}

	if len(store.Devices) == 0 {
		fmt.Println("No paired devices. Run `corgi mcp --http :8765 --pair` to pair one.")
		return
	}
	for _, d := range store.Devices {
		fmt.Printf("%-24s paired %s\n", d.Name, d.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
}

func runMCPDevicesRevoke(_ *cobra.Command, args []string) {
	path := mcpDeviceStorePath()
	store, err := pairing.Load(path)
	if err != nil {
		exitWithError("mcp_devices_read", err, 1)
	}
	if !store.Revoke(args[0]) {
		exitWithError("mcp_device_unknown", fmt.Errorf("no paired device called %q", args[0]), 1)
	}
	if err := pairing.Save(path, store); err != nil {
		exitWithError("mcp_devices_write", err, 1)
	}
	utils.Infof("revoked %s — other devices are unaffected\n", args[0])
}

func init() {
	mcpDevicesCmd.AddCommand(mcpDevicesListCmd, mcpDevicesRevokeCmd)
	mcpCmd.AddCommand(mcpDevicesCmd)
}
