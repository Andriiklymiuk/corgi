package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/rooms"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	queueWaitMax  = 25 * time.Second
	queueWaitStep = 500 * time.Millisecond
)

type taskQueueRequest struct {
	As        string   `json:"as"`
	Tags      []string `json:"tags"`
	Workspace string   `json:"workspace"`
	Lease     string   `json:"lease"`
	Wait      string   `json:"wait"`
	Task      string   `json:"task"`
	Result    string   `json:"result"`
}

func (q taskQueueRequest) owner(r *http.Request) string {
	if device := deviceOf(r); device != "" {
		return device
	}
	if who := strings.TrimSpace(q.As); who != "" {
		return who
	}
	return peers.Me()
}

func queueDuration(raw string, max time.Duration) (time.Duration, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, true
	}
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d < 0 {
		return 0, false
	}
	if max > 0 && d > max {
		d = max
	}
	return d, true
}

func readQueueRequest(w http.ResponseWriter, r *http.Request) (taskQueueRequest, bool) {
	var req taskQueueRequest
	if r.Method != http.MethodPost {
		writeLaunchError(w, http.StatusMethodNotAllowed, "POST {as, tags, workspace, lease, wait}")
		return req, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return req, false
	}
	return req, true
}

func takeNextWaiting(dir string, q watch.NextQuery, wait time.Duration, done <-chan struct{}) (watch.Task, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		t, ok, err := watch.TakeNextTask(dir, q, time.Now())
		if err != nil || ok || !time.Now().Before(deadline) {
			return t, ok, err
		}
		select {
		case <-done:
			return watch.Task{}, false, nil
		case <-time.After(queueWaitStep):
		}
	}
}

func launchTaskNextHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	req, ok := readQueueRequest(w, r)
	if !ok {
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lease, okLease := queueDuration(req.Lease, watch.MaxTaskLease)
	wait, okWait := queueDuration(req.Wait, queueWaitMax)
	if !okLease || !okWait {
		writeLaunchError(w, http.StatusBadRequest, "lease and wait are durations: 1h, 10m, 20s")
		return
	}
	q := watch.NextQuery{Owner: req.owner(r), Tags: req.Tags, Workspace: req.Workspace, Lease: lease}
	t, found, err := takeNextWaiting(dir, q, wait, r.Context().Done())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		writeLaunchJSON(w, map[string]any{"task": nil, "ready": 0})
		return
	}
	nudgeDaemon(dir)
	writeLaunchJSON(w, map[string]any{"ref": t.Ref(), "task": t, "leaseUntil": t.LeaseUntil})
}

func launchTaskRenewHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	req, ok := readQueueRequest(w, r)
	if !ok {
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lease, okLease := queueDuration(req.Lease, watch.MaxTaskLease)
	if !okLease || strings.TrimSpace(req.Task) == "" {
		writeLaunchError(w, http.StatusBadRequest, "POST {task, lease}: lease is a duration like 1h")
		return
	}
	t, err := watch.RenewTask(dir, req.Task, req.owner(r), lease, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusConflict, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"ref": t.Ref(), "leaseUntil": t.LeaseUntil})
}

func launchTaskFinishHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	req, ok := readQueueRequest(w, r)
	if !ok {
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	who := req.owner(r)
	t, found := watch.LoadTasks(dir).Find(req.Task)
	switch {
	case !found:
		writeLaunchError(w, http.StatusNotFound, "no task "+req.Task)
		return
	case !strings.EqualFold(t.Owner, who):
		writeLaunchError(w, http.StatusConflict, t.Ref()+" is held by "+firstNonEmpty(t.Owner, "nobody")+": only they finish it")
		return
	}
	t, err = watch.FinishTask(dir, req.Task, who, req.Result, time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	nudgeDaemon(dir)
	writeLaunchJSON(w, map[string]any{"ref": t.Ref(), "state": t.State})
}

func roomAllowed(r *http.Request, dir, room, action string) (string, bool) {
	device := deviceOf(r)
	if device == "" {
		return "", true
	}
	list := rooms.Grants(dir)
	if !rooms.Covered(list, device) {
		if isViewer(r) {
			return "a read-only device reaches a room only with a grant: corgi agent room grant " + device + " <path>=read", false
		}
		return "", true
	}
	verdict := rooms.Explain(list, device, room, action)
	return verdict.Because, verdict.Allowed
}

func roomsFor(r *http.Request, dir string) []rooms.Info {
	out := []rooms.Info{}
	for _, info := range rooms.List(dir) {
		if _, ok := roomAllowed(r, dir, info.Name, rooms.CanRead); ok {
			out = append(out, info)
		}
	}
	return out
}

func readRoomWaiting(dir string, q rooms.Query, wait time.Duration, done <-chan struct{}) ([]rooms.Message, error) {
	deadline := time.Now().Add(wait)
	for {
		list, err := rooms.Read(dir, q)
		if err != nil || len(list) > 0 || !time.Now().Before(deadline) {
			return list, err
		}
		select {
		case <-done:
			return list, nil
		case <-time.After(queueWaitStep):
		}
	}
}

type roomPost struct {
	Room   string            `json:"room"`
	Text   string            `json:"text"`
	Thread string            `json:"thread"`
	Shape  string            `json:"shape"`
	Data   json.RawMessage   `json:"data"`
	Labels map[string]string `json:"labels"`
	As     string            `json:"as"`
}

func launchRoomsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		launchRoomRead(w, r, dir)
	case http.MethodPost:
		launchRoomPost(w, r, dir)
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET ?room=&after=&thread=&wait=, or POST {room, text, thread, shape, data, labels}")
	}
}

func launchRoomRead(w http.ResponseWriter, r *http.Request, dir string) {
	query := r.URL.Query()
	name := strings.TrimSpace(query.Get("room"))
	if name == "" {
		list, agents := roomsFor(r, dir), roomAgents(dir)
		writeLaunchJSON(w, map[string]any{"rooms": list, "agents": agents, "places": roomPlaces(agents, list), "me": firstNonEmpty(deviceOf(r), peers.Me())})
		return
	}
	room, err := rooms.CleanName(name)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	if why, ok := roomAllowed(r, dir, room, rooms.CanRead); !ok {
		writeLaunchError(w, http.StatusForbidden, why)
		return
	}
	wait, ok := queueDuration(query.Get("wait"), queueWaitMax)
	labels, lerr := rooms.ParseLabels(query["label"])
	if !ok || lerr != nil {
		writeLaunchError(w, http.StatusBadRequest, "wait is a duration like 20s, label is key=value")
		return
	}
	list, err := readRoomWaiting(dir, rooms.Query{Room: room, After: query.Get("after"), Thread: query.Get("thread"), Label: labels}, wait, r.Context().Done())
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeLaunchJSON(w, map[string]any{"room": room, "messages": list})
}

func launchRoomPost(w http.ResponseWriter, r *http.Request, dir string) {
	var req roomPost
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
		writeLaunchError(w, http.StatusBadRequest, requestUnreadableMessage)
		return
	}
	room, err := rooms.CleanName(req.Room)
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	if why, ok := roomAllowed(r, dir, room, rooms.CanPost); !ok {
		writeLaunchError(w, http.StatusForbidden, why)
		return
	}
	by, via := deviceOf(r), roomViaDevice
	if by == "" {
		by, via = firstNonEmpty(strings.TrimSpace(req.As), peers.Me()), ""
	}
	out, err := postToRoom(dir, rooms.Message{Room: room, Thread: req.Thread, By: by, Text: req.Text, Shape: req.Shape, Data: req.Data, Labels: req.Labels, Via: via}, !isViewer(r), time.Now())
	if err != nil {
		writeLaunchError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeLaunchJSON(w, out)
}
