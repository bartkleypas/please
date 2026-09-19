package acp

import (
	"strings"
	"testing"
)

func TestParseClientPrompt_PurePrompt(t *testing.T) {
	raw := "Hello please! How are you?"
	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	if cleaned != raw {
		t.Errorf("expected %q, got %q", raw, cleaned)
	}
	if file != "" || line != 0 || selLines != "" || selCode != "" {
		t.Errorf("expected empty telemetry, got file=%q, line=%d, selLines=%q, selCode=%q", file, line, selLines, selCode)
	}
}

func TestParseClientPrompt_SystemReminderStripped(t *testing.T) {
	raw := "<system-reminder>\nBe concise and follow Swift style.\n</system-reminder>\nWhat is this function doing?"
	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	if cleaned != "What is this function doing?" {
		t.Errorf("unexpected cleaned prompt: %q", cleaned)
	}
	if file != "" || line != 0 || selLines != "" || selCode != "" {
		t.Errorf("expected empty telemetry, got file=%q, line=%d, selLines=%q, selCode=%q", file, line, selLines, selCode)
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

	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	expectedCleaned := "Are you kidding? The path that XCode is providing you from the telemetry you are getting is a _virtual_ view of the _actual_ workspace? I am groaning outloud."
	if cleaned != expectedCleaned {
		t.Errorf("expected cleaned %q, got %q", expectedCleaned, cleaned)
	}
	if file != "" || line != 0 || selLines != "" || selCode != "" {
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

	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
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
	if selLines != "" || selCode != "" {
		t.Errorf("expected empty selection, got selLines=%q, selCode=%q", selLines, selCode)
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

	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	expectedCleaned := "Ok, i switched away from index.md. Can you tell what file i have open now?"
	if cleaned != expectedCleaned {
		t.Errorf("expected pure user prompt %q, got %q", expectedCleaned, cleaned)
	}
	if selLines != "22-23" {
		t.Errorf("expected selectedLines '22-23', got %q", selLines)
	}
	if !strings.Contains(selCode, "SecureField(\"Auth Token (Optional)\"") {
		t.Errorf("expected selectedCode to contain snippet, got:\n%s", selCode)
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

	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	if cleaned != "Can you check this field?" {
		t.Errorf("expected pure user prompt 'Can you check this field?', got %q", cleaned)
	}
	if file != "Sources/PleaseUI/Views/AppSettingsView.swift" {
		t.Errorf("expected file Sources/PleaseUI/Views/AppSettingsView.swift, got %q", file)
	}
	if line != 22 {
		t.Errorf("expected line 22, got %d", line)
	}
	if selLines != "22-23" {
		t.Errorf("expected selected lines '22-23', got %q", selLines)
	}
	if !strings.Contains(selCode, "SecureField(\"Auth Token (Optional)\"") {
		t.Errorf("expected selectedCode to contain snippet, got:\n%s", selCode)
	}
}

func TestParseClientPrompt_SelectionWithoutUserText(t *testing.T) {
	raw := `The user has selected the following code from that file (line 15):
let theme = AppTheme.midnightOwl`

	cleaned, _, line, selLines, selCode := ParseClientPrompt(raw)
	if cleaned != "Please examine the selected code." {
		t.Errorf("expected fallback prompt for selection without text, got %q", cleaned)
	}
	if line != 15 {
		t.Errorf("expected line 15, got %d", line)
	}
	if selLines != "15" {
		t.Errorf("expected selLines '15', got %q", selLines)
	}
	if selCode != "let theme = AppTheme.midnightOwl" {
		t.Errorf("expected selCode 'let theme = AppTheme.midnightOwl', got %q", selCode)
	}
}

func TestParseClientPrompt_XcodeInsideFileNoSelection(t *testing.T) {
	raw := `The user is currently inside this file:
PleasePackage/Sources/PleaseUI/Views/AppSettingsView.swift

The user has no code selected.
Outstanding! Can you tell me where I am currently "looking" in my editor?`

	cleaned, file, line, selLines, selCode := ParseClientPrompt(raw)
	expectedCleaned := `Outstanding! Can you tell me where I am currently "looking" in my editor?`
	if cleaned != expectedCleaned {
		t.Errorf("expected clean prompt %q, got %q", expectedCleaned, cleaned)
	}
	if file != "Sources/PleaseUI/Views/AppSettingsView.swift" {
		t.Errorf("expected normalized file Sources/PleaseUI/Views/AppSettingsView.swift, got %q", file)
	}
	if line != 0 {
		t.Errorf("expected line 0, got %d", line)
	}
	if selLines != "" || selCode != "" {
		t.Errorf("expected empty selection, got selLines=%q, selCode=%q", selLines, selCode)
	}
}
