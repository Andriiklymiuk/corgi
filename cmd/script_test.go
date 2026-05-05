package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestShouldRunScriptManualNoFlag(t *testing.T) {
	t.Cleanup(func() { ScriptNamesFromFlag = nil })
	ScriptNamesFromFlag = nil
	if shouldRunScript(utils.Script{Name: "x", ManualRun: true}) {
		t.Error("want false")
	}
}

func TestShouldRunScriptIncludedInFlag(t *testing.T) {
	t.Cleanup(func() { ScriptNamesFromFlag = nil })
	ScriptNamesFromFlag = []string{"deploy"}
	if !shouldRunScript(utils.Script{Name: "deploy"}) {
		t.Error("want true")
	}
	if shouldRunScript(utils.Script{Name: "other"}) {
		t.Error("want false")
	}
}

func TestShouldRunScriptNoFlagAllowsAll(t *testing.T) {
	t.Cleanup(func() { ScriptNamesFromFlag = nil })
	ScriptNamesFromFlag = nil
	if !shouldRunScript(utils.Script{Name: "x"}) {
		t.Error("want true when no filter")
	}
}

func TestShouldRunScriptManualButForcedByFlag(t *testing.T) {
	t.Cleanup(func() { ScriptNamesFromFlag = nil })
	ScriptNamesFromFlag = []string{"x"}
	if !shouldRunScript(utils.Script{Name: "x", ManualRun: true}) {
		t.Error("want true (flag forces inclusion)")
	}
}

func TestRunServiceScriptEmpty(t *testing.T) {
	runServiceScript(utils.Script{Name: "x", Commands: nil}, "")
}

func TestRunServiceScriptEcho(t *testing.T) {
	runServiceScript(utils.Script{Name: "x", Commands: []string{"echo hi"}}, "")
}

func TestRunScriptsForServiceNoScripts(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	runScriptsForService(corgi, utils.Service{ServiceName: "x"})
}
