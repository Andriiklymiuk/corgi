package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTheSlowestRouteComesFirstWithItsPercentiles(t *testing.T) {
	log := &launchTimings{routes: map[string]*routeSamples{}}
	for i := 1; i <= 100; i++ {
		log.record("/launch/board", time.Duration(i)*time.Millisecond)
	}
	log.record("/launch/info", 2*time.Millisecond)

	rows := log.snapshot()
	if len(rows) != 2 || rows[0].Path != "/launch/board" {
		t.Fatalf("want the board first, got %+v", rows)
	}
	if rows[0].Count != 100 || rows[0].P50 != 51 || rows[0].P95 != 96 || rows[0].Max != 100 {
		t.Fatalf("board percentiles: %+v", rows[0])
	}
}

func TestOnlyTheLastSamplesAreKeptButEveryCallIsCounted(t *testing.T) {
	log := &launchTimings{routes: map[string]*routeSamples{}}
	for i := 0; i < timingKept; i++ {
		log.record("/launch/board", time.Second)
	}
	for i := 0; i < timingKept; i++ {
		log.record("/launch/board", time.Millisecond)
	}
	row := log.snapshot()[0]
	if row.Count != 2*timingKept || row.Max != 1 {
		t.Fatalf("old samples should be gone: %+v", row)
	}
}

func TestATimedRouteRecordsAndSavesAtMostOncePerWindow(t *testing.T) {
	previousLog, previousDir := launchTimingLog, launchTimingsDir
	launchTimingLog = &launchTimings{routes: map[string]*routeSamples{}}
	launchTimingsDir = t.TempDir()
	t.Cleanup(func() { launchTimingLog, launchTimingsDir = previousLog, previousDir })

	mux := http.NewServeMux()
	mux.Handle("/launch/info", timedLaunchRoute("/launch/info", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, mimeJSON)
		_, _ = w.Write([]byte(`{}`))
	})))
	mux.HandleFunc("/launch/timings", launchTimingsHandler)
	srv := httptest.NewServer(launchCompression(mux))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/launch/info", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}

	file := filepath.Join(launchTimingsDir, timingsFileName)
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk struct {
		Routes []routeTiming `json:"routes"`
	}
	if err := json.Unmarshal(saved, &onDisk); err != nil || len(onDisk.Routes) != 1 || onDisk.Routes[0].Count != 1 {
		t.Fatalf("the file holds the first save only: %s (%v)", saved, err)
	}

	res, err := srv.Client().Get(srv.URL + "/launch/timings")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var live struct {
		Routes []routeTiming `json:"routes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&live); err != nil || len(live.Routes) != 1 || live.Routes[0].Count != 3 {
		t.Fatalf("the live answer counts all three: %+v (%v)", live, err)
	}
}
