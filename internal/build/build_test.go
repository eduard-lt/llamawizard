package build

import (
	"strings"
	"testing"
)

func TestCheckDeps_ReturnsAllFive(t *testing.T) {
	statuses := CheckDeps()
	if len(statuses) != 5 {
		t.Fatalf("expected 5 dependencies, got %d", len(statuses))
	}

	expectedNames := []string{"git", "Homebrew", "Xcode CLT", "cmake", "uv"}
	for i, s := range statuses {
		if s.Name != expectedNames[i] {
			t.Errorf("statuses[%d].Name = %q, want %q", i, s.Name, expectedNames[i])
		}
	}
}

func TestCheckDeps_GitPresent(t *testing.T) {
	statuses := CheckDeps()
	for _, s := range statuses {
		if s.Name == "git" {
			if !s.Present {
				t.Error("git should be present on this machine")
			}
			if s.Path == "" {
				t.Error("git Path should not be empty when present")
			}
			return
		}
	}
	t.Fatal("did not find git in statuses")
}

func TestCheckDeps_PathNotEmptyWhenPresent(t *testing.T) {
	statuses := CheckDeps()
	for _, s := range statuses {
		if s.Present && s.Path == "" {
			t.Errorf("%s is present but Path is empty", s.Name)
		}
		if !s.Present && s.Path != "" {
			t.Errorf("%s is missing but Path is %q", s.Name, s.Path)
		}
	}
}

func TestCheckTool_Existing(t *testing.T) {
	s := checkTool("ls")
	if !s.Present {
		t.Error("'ls' should be found on PATH")
	}
	if s.Path == "" {
		t.Error("'ls' path should not be empty")
	}
}

func TestCheckTool_Nonexistent(t *testing.T) {
	s := checkTool("nonexistent_tool_xyz_12345")
	if s.Present {
		t.Error("nonexistent tool should not be found")
	}
	if s.Path != "" {
		t.Errorf("nonexistent tool path should be empty, got %q", s.Path)
	}
}

func TestMissing(t *testing.T) {
	present := DepStatus{Name: "foo", Present: true}
	absent := DepStatus{Name: "bar", Present: false}

	if present.Missing() {
		t.Error("present dep should not report Missing")
	}
	if !absent.Missing() {
		t.Error("absent dep should report Missing")
	}
}

func TestAllPresent(t *testing.T) {
	allGood := []DepStatus{
		{Name: "a", Present: true},
		{Name: "b", Present: true},
	}
	someMissing := []DepStatus{
		{Name: "a", Present: true},
		{Name: "b", Present: false},
	}
	none := []DepStatus{}

	if !AllPresent(allGood) {
		t.Error("all present should return true")
	}
	if AllPresent(someMissing) {
		t.Error("some missing should return false")
	}
	if !AllPresent(none) {
		t.Error("empty slice should return true (vacuously)")
	}
}

func TestSummary(t *testing.T) {
	statuses := []DepStatus{
		{Name: "a", Present: true},
		{Name: "b", Present: false},
	}
	out := Summary(statuses)
	if out == "" {
		t.Error("Summary should not be empty")
	}
	// Should contain both names and the separator
	if len(out) < 10 {
		t.Errorf("Summary too short: %q", out)
	}
}

func TestInstallMissing_NothingMissing(t *testing.T) {
	statuses := []DepStatus{
		{Name: "Homebrew", Present: true},
		{Name: "Xcode CLT", Present: true},
		{Name: "cmake", Present: true},
		{Name: "git", Present: true},
		{Name: "uv", Present: true},
	}
	err := InstallMissing(statuses)
	if err != nil {
		t.Errorf("InstallMissing with all present should return nil, got %v", err)
	}
}

func TestInstallMissing_HomebrewMissing(t *testing.T) {
	statuses := []DepStatus{
		{Name: "Homebrew", Present: false},
		{Name: "Xcode CLT", Present: true},
		{Name: "cmake", Present: false},
		{Name: "git", Present: true},
		{Name: "uv", Present: true},
	}
	err := InstallMissing(statuses)
	if err == nil {
		t.Fatal("expected ErrHomebrewMissing, got nil")
	}
	if _, ok := err.(ErrHomebrewMissing); !ok {
		t.Errorf("expected ErrHomebrewMissing, got %T: %v", err, err)
	}
}

func TestInstallMissing_XcodeCLTMissing(t *testing.T) {
	statuses := []DepStatus{
		{Name: "Homebrew", Present: true},
		{Name: "Xcode CLT", Present: false},
		{Name: "cmake", Present: true},
		{Name: "git", Present: true},
		{Name: "uv", Present: true},
	}
	err := InstallMissing(statuses)
	if err == nil {
		t.Fatal("expected ErrXcodeCLTMissing, got nil")
	}
	if _, ok := err.(ErrXcodeCLTMissing); !ok {
		t.Errorf("expected ErrXcodeCLTMissing, got %T: %v", err, err)
	}
}

func TestInstallMissing_BrewInstall(t *testing.T) {
	// Simulate missing cmake and uv (both already installed on this machine,
	// so "brew install" will succeed quickly as idempotent).
	statuses := []DepStatus{
		{Name: "Homebrew", Present: true},
		{Name: "Xcode CLT", Present: true},
		{Name: "cmake", Present: false},
		{Name: "git", Present: true},
		{Name: "uv", Present: false},
	}
	err := InstallMissing(statuses)
	if err != nil {
		t.Errorf("InstallMissing should succeed with brew install, got %v", err)
	}

	// Verify they are now detected as present.
	fresh := CheckDeps()
	for _, s := range fresh {
		if (s.Name == "cmake" || s.Name == "uv") && !s.Present {
			t.Errorf("%s should be present after InstallMissing", s.Name)
		}
	}
}

func TestInstallMissing_HomebrewTakesPriority(t *testing.T) {
	// Both Homebrew and Xcode CLT missing — Homebrew error should win.
	statuses := []DepStatus{
		{Name: "Homebrew", Present: false},
		{Name: "Xcode CLT", Present: false},
		{Name: "cmake", Present: false},
		{Name: "git", Present: true},
		{Name: "uv", Present: true},
	}
	err := InstallMissing(statuses)
	if _, ok := err.(ErrHomebrewMissing); !ok {
		t.Errorf("expected ErrHomebrewMissing (priority), got %T: %v", err, err)
	}
}

func TestErrHomebrewMissing_Error(t *testing.T) {
	err := ErrHomebrewMissing{}
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should not be empty")
	}
	if !strings.Contains(msg, "homebrew") {
		t.Errorf("error message should mention homebrew: %q", msg)
	}
}

func TestErrXcodeCLTMissing_Error(t *testing.T) {
	err := ErrXcodeCLTMissing{}
	msg := err.Error()
	if msg == "" {
		t.Error("Error() should not be empty")
	}
	if !strings.Contains(msg, "xcode") {
		t.Errorf("error message should mention xcode: %q", msg)
	}
}
