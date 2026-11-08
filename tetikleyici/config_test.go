package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigReadsEveryField(t *testing.T) {
	set, err := parseConfig([]byte(`
opencode: "~/.opencode/bin/opencode"
rules:
  - name: everyone
    match:
      everyone: true
    do:
      - type: command
        code: "~/bin/a.sh"
  - name: yusuf
    match:
      name: "Yusuf"
      number: ["905550000003", "12345678901236"]
    do:
      - type: session
        session: "ses_abc"
  - name: question
    match:
      text: "mufredat"
    do:
      - type: new_session
`))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if set.OpenCode != "~/.opencode/bin/opencode" {
		t.Errorf("opencode = %q", set.OpenCode)
	}
	if len(set.Rules) != 3 {
		t.Fatalf("rule = %d, want 3", len(set.Rules))
	}

	first := set.Rules[0]
	if first.Name != "everyone" || !first.Match.Everyone {
		t.Errorf("rule 0 = %+v", first)
	}
	if first.Actions[0].Kind != "command" || first.Actions[0].Code != "~/bin/a.sh" {
		t.Errorf("action 0 = %+v", first.Actions[0])
	}

	second := set.Rules[1]
	if len(second.Match.Numbers) != 2 || second.Match.Numbers[0] != "905550000003" {
		t.Errorf("number = %v", second.Match.Numbers)
	}
	if second.Actions[0].Kind != "session" || second.Actions[0].SessionID != "ses_abc" {
		t.Errorf("action 1 = %+v", second.Actions[0])
	}

	if set.Rules[2].Actions[0].Kind != "new_session" {
		t.Errorf("action 2 = %+v", set.Rules[2].Actions[0])
	}
}

func TestParseConfigRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"half YAML", `rules: [`, "did not find expected node content"},
		{"unknown action", `rules:
- name: a
  match: { everyone: true }
  do: [ { type: "uydurma" } ]`, "unknown action type"},
		{"command without code", `rules:
- name: a
  match: { everyone: true }
  do: [ { type: "command" } ]`, `code required`},
		{"session without id", `rules:
- name: ad
  match: { everyone: true }
  do: [ { type: "session" } ]`, `session required (find with opencode session list)`},
		{"actionless rule", `rules:
- name: a
  match: { everyone: true }
  do: []`, "at least one action required"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig([]byte(tc.raw))
			if err == nil {
				t.Fatal("should be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should contain %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestRuleNameFallsBackToIndex(t *testing.T) {
	set, err := parseConfig([]byte(`
rules:
  - match: { everyone: true }
    do: [ { type: command, code: "a" } ]
  - name: named
    match: { everyone: true }
    do: [ { type: command, code: "b" } ]`))
	if err != nil {
		t.Fatal(err)
	}

	if set.Rules[0].name() != "#1" {
		t.Errorf("unnamed rule = %q, want #1", set.Rules[0].name())
	}
	if set.Rules[1].name() != "named" {
		t.Errorf("named rule = %q", set.Rules[1].name())
	}
}

func TestActionLabelIsReadable(t *testing.T) {
	cases := []struct {
		action action
		want   string
	}{
		{action{Kind: "command", Code: "echo hello"}, "command: echo hello"},
		{action{Kind: "session", SessionID: "ses_x"}, "session: ses_x"},
		{action{Kind: "new_session"}, "new_session"},
	}

	for _, tc := range cases {
		if got := tc.action.label(); got != tc.want {
			t.Errorf("label = %q, want %q", got, tc.want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("home not available")
	}

	if got := expandHome("~/bin/x"); got != filepath.Join(home, "bin", "x") {
		t.Errorf("expandHome(~) = %q", got)
	}
	if got := expandHome("~"); got != home {
		t.Errorf("expandHome(~ only) = %q", got)
	}
	if got := expandHome("/usr/bin/env"); got != "/usr/bin/env" {
		t.Errorf("absolute path should not change, got %q", got)
	}
}

func TestLoadConfigReadsFileAndFailsLoudlyWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.yaml")
	body := `rules:
- name: a
  match: { everyone: true }
  do: [ { type: command, code: "x" } ]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	set, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(set.Rules) != 1 {
		t.Errorf("rule = %d", len(set.Rules))
	}

	_, err = loadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("missing file should error")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error should mention read failure, got: %v", err)
	}
}
