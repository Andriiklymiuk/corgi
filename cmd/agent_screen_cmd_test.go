package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/screen"
	"andriiklymiuk/corgi/utils/agent/timers"
)

type exitCalled struct{ code int }

func exitsWith(t *testing.T, run func()) (code int) {
	t.Helper()
	prev := osExit
	osExit = func(c int) { panic(exitCalled{c}) }
	defer func() {
		osExit = prev
		if r := recover(); r != nil {
			e, ok := r.(exitCalled)
			if !ok {
				panic(r)
			}
			code = e.code
		}
	}()
	run()
	return -1
}

func TestScreenCommandsRun(t *testing.T) {
	rig := newScreenRig(t)
	code := -1

	agentScreenCmd.Run(agentScreenCmd, []string{"on"})
	screenHostPort, screenHostFastPort, screenHostFingerprint = 4300, 4301, strings.Repeat("ef", 32)
	t.Cleanup(func() { screenHostPort, screenHostFastPort, screenHostFingerprint, screenHostClear = 0, 0, "", false })
	agentScreenHostCmd.Run(agentScreenHostCmd, nil)
	if h, _ := screen.CurrentHost(rig.dir, time.Now()); h == nil || h.Port != 4300 {
		t.Fatalf("host: %+v", h)
	}

	agentScreenSettingsCmd.Run(agentScreenSettingsCmd, []string{`{"do":"grant","device":"` + rig.hashes["ipad"] + `","role":"control"}`})
	agentScreenSettingsCmd.Run(agentScreenSettingsCmd, nil)
	screenTicketDevice = rig.hashes["ipad"]
	t.Cleanup(func() { screenTicketDevice = "" })
	if c := exitsWith(t, func() { agentScreenTicketCmd.Run(agentScreenTicketCmd, nil) }); c != -1 {
		t.Fatalf("ticket exited %d", c)
	}

	tk, _ := screen.MintTicket(rig.dir, rig.hashes["ipad"], "iPad mini", time.Now())
	screenVerifyTicket, screenVerifyAddr = tk, "192.168.1.3:5000"
	t.Cleanup(func() { screenVerifyTicket, screenVerifyAddr = "", "" })
	agentScreenVerifyCmd.Run(agentScreenVerifyCmd, nil)
	if recent, _ := screen.Recent(rig.dir, 5); len(recent) == 0 || recent[0].Event != "start" {
		t.Fatalf("verify logged a start: %+v", recent)
	}

	screenHostClear = true
	agentScreenHostCmd.Run(agentScreenHostCmd, nil)
	if h, _ := screen.CurrentHost(rig.dir, time.Now()); h != nil {
		t.Fatal("clear")
	}
	agentScreenCmd.Run(agentScreenCmd, []string{"off"})
	screenHostClear = false
	agentScreenHostCmd.Run(agentScreenHostCmd, nil)
	if h, _ := screen.CurrentHost(rig.dir, time.Now()); h != nil {
		t.Fatal("no host while off")
	}
	if code = exitsWith(t, func() { agentScreenTicketCmd.Run(agentScreenTicketCmd, nil) }); code != 2 {
		t.Fatalf("a ticket while off exits 2, got %d", code)
	}
	if code = exitsWith(t, func() { agentScreenSettingsCmd.Run(agentScreenSettingsCmd, []string{"{"}) }); code != 2 {
		t.Fatalf("bad settings json exits 2, got %d", code)
	}
	if code = exitsWith(t, func() { agentScreenCmd.Run(agentScreenCmd, []string{"explode"}) }); code != 1 {
		t.Fatalf("unknown word exits 1, got %d", code)
	}
}

func TestLaterAndDreamCommandsRun(t *testing.T) {
	rig := newScreenRig(t)

	agentLaterCmd.Run(agentLaterCmd, []string{"2h", "check", "the", "PR"})
	if list, _ := timers.List(rig.dir); len(list) != 1 || list[0].Text != "check the PR" {
		t.Fatalf("timers: %+v", list)
	}
	agentLaterCmd.Run(agentLaterCmd, nil)
	if code := exitsWith(t, func() { agentLaterCmd.Run(agentLaterCmd, []string{"whenever"}) }); code != 2 {
		t.Fatalf("bad time exits 2, got %d", code)
	}

	_ = lessons.Add(rig.dir, "api", lessons.Lesson{At: time.Now().AddDate(-1, 0, 0), Source: "s", Text: "old lesson"})
	if err := agentLessonDreamCmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}
	agentLessonDreamCmd.Run(agentLessonDreamCmd, nil)
	if len(lessons.List(rig.dir, "api")) != 1 {
		t.Fatal("dry run keeps the lesson")
	}
	_ = agentLessonDreamCmd.Flags().Set("dry-run", "false")
	agentLessonDreamCmd.Run(agentLessonDreamCmd, nil)
	if len(lessons.List(rig.dir, "api")) != 0 {
		t.Fatal("dream put the old lesson away")
	}
}
