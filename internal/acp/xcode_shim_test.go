package acp

import (
	"strings"
	"testing"
)

func TestParseClientPrompt_PurePrompt(t *testing.T) {
	raw := "Hello please! How are you?"
	cleaned, file, line := ParseClientPrompt(raw)
	if cleaned != raw {
		t.Errorf("expected %q, got %q", raw, cleaned)
	}
	if file != "" || line != 0 {
		t.Errorf("expected empty telemetry, got file=%q, line=%d", file, line)
	}
}

func TestParseClientPrompt_SystemReminderStripped(t *testing.T) {
	raw := "<system-reminder>\nBe concise and follow Swift style.\n</system-reminder>\nWhat is this function doing?"
	cleaned, file, line := ParseClientPrompt(raw)
	if cleaned != "What is this function doing?" {
		t.Errorf("unexpected cleaned prompt: %q", cleaned)
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

	cleaned, file, line := ParseClientPrompt(raw)
	expectedCleaned := "Are you kidding? The path that XCode is providing you from the telemetry you are getting is a _virtual_ view of the _actual_ workspace? I am groaning outloud."
	if cleaned != expectedCleaned {
		t.Errorf("expected cleaned %q, got %q", expectedCleaned, cleaned)
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

	cleaned, file, line := ParseClientPrompt(raw)
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
}

func TestParseClientPrompt_XcodeWithCodeSelection(t *testing.T) {
	raw := `<system-reminder>## Xcode
</system-reminder>Project structure (these are Xcode workspace-relative paths and do not correspond to filesystem paths. Use XcodeRead, XcodeWrite, XcodeGrep, and XcodeGlob to interact with these files):
OwlPlease/Sources/PleaseApp/Assets.xcassets
OwlPlease/Sources/PleaseApp/PleaseApp.swift

Package dependencies: PleasePackage

The user has selected the following code from that file (lines 22-23):
          SecureField("Auth Token (Optional)", text: $viewModel.authToken)
            .textFieldStyle(.roundedBorder)
Ok, i switched away from index.md. Can you tell what file i have open now?`

	cleaned, file, line := ParseClientPrompt(raw)
	if !strings.Contains(cleaned, "[Selected code (lines 22-23)]:") {
		t.Errorf("expected cleaned prompt to contain formatted selection header, got:\n%s", cleaned)
	}
	if !strings.Contains(cleaned, "SecureField(\"Auth Token (Optional)\"") {
		t.Errorf("expected cleaned prompt to contain code snippet, got:\n%s", cleaned)
	}
	if !strings.Contains(cleaned, "Ok, i switched away from index.md") {
		t.Errorf("expected cleaned prompt to contain user question, got:\n%s", cleaned)
	}
	if line != 22 {
		t.Errorf("expected cursor line 22 from selection, got %d", line)
	}
	if file == "selected" {
		t.Errorf("active file must never be the keyword 'selected'")
	}
}

func TestParseClientPrompt_XcodeWithOpenFileAndSelection(t *testing.T) {
	raw := `Project structure (these are Xcode workspace-relative paths and do not correspond to filesystem paths. Use XcodeRead, XcodeWrite, XcodeGrep, and XcodeGlob to interact with these files):
OwlPlease/Sources/PleaseApp/PleaseApp.swift

Package dependencies: PleasePackage

The user is looking at file Sources/PleaseUI/Views/AppSettingsView.swift at line 22.
The user has selected the following code from that file (lines 22-23):
          SecureField("Auth Token (Optional)", text: $viewModel.authToken)
            .textFieldStyle(.roundedBorder)
Can you check this field?`

	cleaned, file, line := ParseClientPrompt(raw)
	if file != "Sources/PleaseUI/Views/AppSettingsView.swift" {
		t.Errorf("expected file Sources/PleaseUI/Views/AppSettingsView.swift, got %q", file)
	}
	if line != 22 {
		t.Errorf("expected line 22, got %d", line)
	}
	if !strings.Contains(cleaned, "[Selected code (lines 22-23)]:") {
		t.Errorf("expected selection header, got:\n%s", cleaned)
	}
}
