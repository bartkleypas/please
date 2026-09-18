package acp

import (
	"testing"
)

func TestParseClientPrompt_PurePrompt(t *testing.T) {
	raw := "Hello please! How are you?"
	cleaned, reminder, file, line := ParseClientPrompt(raw)
	if cleaned != raw {
		t.Errorf("expected %q, got %q", raw, cleaned)
	}
	if reminder != "" {
		t.Errorf("expected empty reminder, got %q", reminder)
	}
	if file != "" || line != 0 {
		t.Errorf("expected empty telemetry, got file=%q, line=%d", file, line)
	}
}

func TestParseClientPrompt_SystemReminder(t *testing.T) {
	raw := "<system-reminder>\nBe concise and follow Swift style.\n</system-reminder>\nWhat is this function doing?"
	cleaned, reminder, file, line := ParseClientPrompt(raw)
	if cleaned != "What is this function doing?" {
		t.Errorf("unexpected cleaned prompt: %q", cleaned)
	}
	if reminder != "Be concise and follow Swift style." {
		t.Errorf("unexpected reminder: %q", reminder)
	}
	if file != "" || line != 0 {
		t.Errorf("expected empty telemetry, got file=%q, line=%d", file, line)
	}
}

func TestParseClientPrompt_XcodeFullPreamble(t *testing.T) {
	raw := `<system-reminder>## Xcode

You are currently being called from inside Xcode, the IDE for Apple programming languages and platforms.
</system-reminder>Project structure (these are Xcode workspace-relative paths and do not correspond to filesystem paths. Use XcodeRead, XcodeWrite, XcodeGrep, and XcodeGlob to interact with these files):
OwlPlease/Sources/PleaseApp/Assets.xcassets
OwlPlease/Sources/PleaseApp/PleaseApp.swift

Package dependencies: PleasePackage

The user has no file currently open.
Are you kidding? The path that XCode is providing you from the telemetry you are getting is a _virtual_ view of the _actual_ workspace? I am groaning outloud.`

	cleaned, reminder, file, line := ParseClientPrompt(raw)
	expectedCleaned := "Are you kidding? The path that XCode is providing you from the telemetry you are getting is a _virtual_ view of the _actual_ workspace? I am groaning outloud."
	if cleaned != expectedCleaned {
		t.Errorf("expected cleaned %q, got %q", expectedCleaned, cleaned)
	}
	if reminder == "" {
		t.Errorf("expected non-empty extracted reminder")
	}
	if file != "" || line != 0 {
		t.Errorf("expected no open file, got file=%q, line=%d", file, line)
	}
}

func TestParseClientPrompt_XcodeWithOpenFile(t *testing.T) {
	raw := `<system-reminder>## Xcode
</system-reminder>Project structure (these are Xcode workspace-relative paths and do not correspond to filesystem paths. Use XcodeRead, XcodeWrite, XcodeGrep, and XcodeGlob to interact with these files):
OwlPlease/Sources/PleaseApp/PleaseApp.swift

Package dependencies: PleasePackage

The user is looking at file Sources/PleaseApp/PleaseApp.swift at line 42.
Can you explain how this view is initialized?`

	cleaned, reminder, file, line := ParseClientPrompt(raw)
	expectedCleaned := "Can you explain how this view is initialized?"
	if cleaned != expectedCleaned {
		t.Errorf("expected cleaned %q, got %q", expectedCleaned, cleaned)
	}
	if file != "Sources/PleaseApp/PleaseApp.swift" {
		t.Errorf("expected file Sources/PleaseApp/PleaseApp.swift, got %q", file)
	}
	if line != 42 {
		t.Errorf("expected line 42, got %d", line)
	}
	if reminder == "" {
		t.Errorf("expected extracted reminder")
	}
}
