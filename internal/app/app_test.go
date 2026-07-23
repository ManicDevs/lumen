package app

import (
	"os"
	"testing"
)

func TestParseFlags_Defaults(t *testing.T) {
	f := ParseFlags([]string{})
	if f.AutoMode {
		t.Error("expected AutoMode=false")
	}
	if f.TargetPath != "" {
		t.Errorf("expected empty TargetPath, got %q", f.TargetPath)
	}
}

func TestParseFlags_AutoWithGoal(t *testing.T) {
	f := ParseFlags([]string{"--auto", "fix the bugs"})
	if !f.AutoMode {
		t.Error("expected AutoMode=true")
	}
	if f.AutoGoal != "fix the bugs" {
		t.Errorf("expected goal 'fix the bugs', got %q", f.AutoGoal)
	}
}

func TestParseFlags_AutoNoGoal(t *testing.T) {
	f := ParseFlags([]string{"--auto"})
	if !f.AutoMode {
		t.Error("expected AutoMode=true")
	}
	if f.AutoGoal != "" {
		t.Errorf("expected empty goal, got %q", f.AutoGoal)
	}
}

func TestParseFlags_LiveOutput(t *testing.T) {
	f := ParseFlags([]string{"--auto", "test", "--live-output"})
	if !f.LiveOutput {
		t.Error("expected LiveOutput=true")
	}
}

func TestParseFlags_TargetPath(t *testing.T) {
	f := ParseFlags([]string{"/some/path"})
	if f.TargetPath != "/some/path" {
		t.Errorf("expected TargetPath='/some/path', got %q", f.TargetPath)
	}
}

func TestParseFlags_AutoSandbox(t *testing.T) {
	f := ParseFlags([]string{"--auto-sandbox", "/path"})
	if !f.AutoSandbox {
		t.Error("expected AutoSandbox=true")
	}
	if f.TargetPath != "/path" {
		t.Errorf("expected TargetPath='/path', got %q", f.TargetPath)
	}
}

func TestParseFlags_TrainAndDataset(t *testing.T) {
	tests := []struct {
		args     []string
		name     string
		check    func(Flags) bool
	}{
		{[]string{"--train"}, "--train", func(f Flags) bool { return f.Train && !f.TrainAll }},
		{[]string{"--train-all"}, "--train-all", func(f Flags) bool { return f.Train && f.TrainAll }},
		{[]string{"--dataset-init"}, "--dataset-init", func(f Flags) bool { return f.DatasetInit }},
		{[]string{"--easter-egg"}, "--easter-egg", func(f Flags) bool { return f.EasterEgg }},
		{[]string{"--chat"}, "--chat", func(f Flags) bool { return f.Chat }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := ParseFlags(tt.args)
			if !tt.check(f) {
				t.Errorf("ParseFlags(%v) check failed: %+v", tt.args, f)
			}
		})
	}
}

func TestParseFlags_Topic(t *testing.T) {
	f := ParseFlags([]string{"--topic", "code review"})
	if f.CustomTopic != "code review" {
		t.Errorf("expected topic 'code review', got %q", f.CustomTopic)
	}
}

func TestParseFlags_HelpExits(t *testing.T) {
	if os.Getenv("TEST_HELP_EXIT") == "1" {
		ParseFlags([]string{"--help"})
		return
	}
}

func TestPrintUsage_DoesNotPanic(t *testing.T) {
	PrintUsage()
}
