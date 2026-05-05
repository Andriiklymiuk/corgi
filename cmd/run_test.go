package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestHasDatabaseToRun(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if hasDatabaseToRun(nil) {
			t.Error("want false for nil")
		}
		if hasDatabaseToRun([]utils.DatabaseService{}) {
			t.Error("want false for empty")
		}
	})
	t.Run("all manual returns false", func(t *testing.T) {
		dbs := []utils.DatabaseService{
			{ServiceName: "a", ManualRun: true},
			{ServiceName: "b", ManualRun: true},
		}
		if hasDatabaseToRun(dbs) {
			t.Error("want false when all manual")
		}
	})
	t.Run("at least one non-manual returns true", func(t *testing.T) {
		dbs := []utils.DatabaseService{
			{ServiceName: "a", ManualRun: true},
			{ServiceName: "b", ManualRun: false},
		}
		if !hasDatabaseToRun(dbs) {
			t.Error("want true")
		}
	})
}

func TestOmitServiceCmd(t *testing.T) {
	t.Cleanup(func() { omitItems = nil })
	omitItems = []string{"beforeStart", "afterStart"}
	if !omitServiceCmd("beforeStart") {
		t.Error("want true")
	}
	if !omitServiceCmd("afterStart") {
		t.Error("want true")
	}
	if omitServiceCmd("start") {
		t.Error("want false")
	}
}

func TestGetServiceEnv(t *testing.T) {
	t.Run("nil AutoSourceEnv returns EnvPath", func(t *testing.T) {
		got := getServiceEnv(utils.Service{EnvPath: ".env.local"})
		if got != ".env.local" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("AutoSourceEnv true returns EnvPath", func(t *testing.T) {
		on := true
		got := getServiceEnv(utils.Service{AutoSourceEnv: &on, EnvPath: ".env"})
		if got != ".env" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("AutoSourceEnv false returns sentinel", func(t *testing.T) {
		off := false
		got := getServiceEnv(utils.Service{AutoSourceEnv: &off, EnvPath: ".env"})
		if got != utils.SkipAutoSourceEnv {
			t.Errorf("got %q want SkipAutoSourceEnv", got)
		}
	})
}

func TestShouldSkipManualRun(t *testing.T) {
	t.Cleanup(func() { utils.ServicesItemsFromFlag = nil })

	t.Run("non-manual not skipped", func(t *testing.T) {
		utils.ServicesItemsFromFlag = nil
		if shouldSkipManualRun(utils.Service{ServiceName: "x"}) {
			t.Error("want false")
		}
	})

	t.Run("manual + no flag = skip", func(t *testing.T) {
		utils.ServicesItemsFromFlag = nil
		if !shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want true")
		}
	})

	t.Run("manual but in flag = run", func(t *testing.T) {
		utils.ServicesItemsFromFlag = []string{"x"}
		if shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want false (included in flag)")
		}
	})

	t.Run("manual and flag set but not included = skip", func(t *testing.T) {
		utils.ServicesItemsFromFlag = []string{"y"}
		if !shouldSkipManualRun(utils.Service{ServiceName: "x", ManualRun: true}) {
			t.Error("want true")
		}
	})
}

func TestStartDatabaseIfNeededManualRunNoop(t *testing.T) {
	startDatabaseIfNeeded(utils.DatabaseService{ManualRun: true, ServiceName: "x"})
}
