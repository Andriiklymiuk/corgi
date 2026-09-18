package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"andriiklymiuk/corgi/utils"
)

const DefaultPulseEvery = 5 * time.Minute

type PulseState struct {
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
}

func pulsePath(dir string) string { return filepath.Join(dir, "pulse.json") }

func ReadPulse(dir string) PulseState {
	var st PulseState
	if data, err := os.ReadFile(pulsePath(dir)); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

// A dead-man switch: the URL belongs to a service that alarms when the pings
// stop, so a laptop that died in a bag is noticed by someone.
func (d *Daemon) pulse(ctx context.Context) {
	if d.PulseURL == "" {
		return
	}
	every := d.PulseEvery
	if every <= 0 {
		every = DefaultPulseEvery
	}
	client := &http.Client{Timeout: 10 * time.Second}
	failing := false
	ping := func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.PulseURL, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "corgi-agent/"+d.Version)
		st := PulseState{At: time.Now().UTC()}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			}
		}
		if err != nil {
			st.Error = err.Error()
			if !failing {
				utils.Infof("agent: pulse did not land: %v\n", err)
			}
		} else if failing {
			utils.Info("agent: pulse lands again")
		}
		failing = err != nil
		if data, e := json.Marshal(st); e == nil {
			_ = os.WriteFile(pulsePath(d.Dir), data, 0o600)
		}
	}
	ping()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ping()
		}
	}
}
