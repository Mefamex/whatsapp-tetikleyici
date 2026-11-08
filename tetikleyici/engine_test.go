package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeRules(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnginePicksUpConfigChangesWithoutRestart(t *testing.T) {
	original := `rules:
- name: before
  match: { name: "Mefamex" }
  do: [ { type: command, code: "true" } ]`
	path := writeRules(t, original)

	e := newEngine(path, testOptions())
	if first := e.rules(); first.Rules[0].Name != "before" {
		t.Fatalf("first rule = %q", first.Rules[0].Name)
	}

	updated := strings.Replace(original, `name: "Mefamex"`, `name: "Yusuf"`, 1)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	if second := e.rules(); second.Rules[0].Match.Names[0] != "Yusuf" {
		t.Errorf("change not picked up: %v", second.Rules[0].Match.Names)
	}
}

func TestEngineKeepsLastGoodRulesOnBrokenEdit(t *testing.T) {
	path := writeRules(t, `rules:
- name: working
  match: { everyone: true }
  do: [ { type: command, code: "true" } ]`)

	e := newEngine(path, testOptions())
	e.rules()

	if err := os.WriteFile(path, []byte(`rules: [`), 0o600); err != nil {
		t.Fatal(err)
	}

	after := e.rules()
	if len(after.Rules) != 1 {
		t.Fatalf("broken config should not lose rules, got %d", len(after.Rules))
	}
	if after.Rules[0].Name != "working" {
		t.Errorf("last good should be kept, got %q", after.Rules[0].Name)
	}
}

func TestEngineSurvivesDeletedConfig(t *testing.T) {
	path := writeRules(t, `rules:
- name: a
  match: { everyone: true }
  do: [ { type: command, code: "x" } ]`)

	e := newEngine(path, testOptions())
	e.rules()

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	after := e.rules()
	if len(after.Rules) != 1 {
		t.Errorf("rules = %d, want 1", len(after.Rules))
	}
}

func TestEngineReadsOpencodePathFromConfig(t *testing.T) {
	path := writeRules(t, `opencode: "~/.opencode/bin/opencode"
rules:
- name: a
  match: { everyone: true }
  do: [ { type: command, code: "x" } ]`)

	e := newEngine(path, testOptions())
	e.rules()

	if e.opts.opencode != "~/.opencode/bin/opencode" {
		t.Errorf("opencode = %q", e.opts.opencode)
	}
}

func TestEngineSnapshotDoesNotRereadDisk(t *testing.T) {
	path := writeRules(t, `rules:
- name: first
  match: { everyone: true }
  do: [ { type: command, code: "x" } ]`)
	e := newEngine(path, testOptions())

	if got := e.snapshot(); len(got.Rules) != 0 {
		t.Errorf("snapshot should not read disk, got %d rules", len(got.Rules))
	}

	e.rules()

	if err := os.WriteFile(path, []byte(`rules: []`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := e.snapshot(); got.Rules[0].Name != "first" {
		t.Errorf("snapshot = %q", got.Rules[0].Name)
	}
}

func TestSeenStoreRemembersAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.txt")

	first := newSeenStore(path)
	if first.alreadyHandled("W1") {
		t.Error("first open should not show handled")
	}
	first.mark("W1")

	second := newSeenStore(path)
	if !second.alreadyHandled("W1") {
		t.Error("file id should be read on reopen")
	}
}

func TestSeenStoreIgnoresEmptyIDs(t *testing.T) {
	s := newSeenStore(filepath.Join(t.TempDir(), "seen.txt"))

	s.mark("")
	if s.alreadyHandled("") {
		t.Error("empty id should not be marked handled")
	}
	if s.count() != 0 {
		t.Errorf("count = %d", s.count())
	}
}

func TestChatWatchDeduplicates(t *testing.T) {
	w := newChatWatch()
	w.remember("a@lid")
	w.remember("a@lid")
	w.remember("b@lid")
	w.remember("")

	if got := w.known(); len(got) != 2 {
		t.Errorf("known = %v, want 2", got)
	}
}

func TestKeepTriggerPrefersTheLiveCopy(t *testing.T) {
	fromHistory := []message{{ID: "W1", Text: "", PushName: "Mefamex"}}
	live := message{ID: "W1", Text: "live text", PushName: ""}

	got := keepTrigger(fromHistory, live)

	if got.PushName != "Mefamex" {
		t.Errorf("pushName = %q, want Mefamex", got.PushName)
	}
	if got.Text != "live text" {
		t.Errorf("text = %q", got.Text)
	}
}

func TestWithTriggerAddsMissingTrigger(t *testing.T) {
	trigger := mefamexMessage()

	history := []message{{ID: "old", Timestamp: 1}}
	got := withTrigger(history, trigger)

	if len(got) != 2 || got[0].ID != "W1" {
		t.Errorf("trigger should be prepended, got %v", got)
	}

	if got := withTrigger([]message{trigger}, trigger); len(got) != 1 {
		t.Errorf("duplicate should not be added, got %d", len(got))
	}
}

func TestSortOldestFirst(t *testing.T) {
	in := []message{
		{ID: "c", Timestamp: 300},
		{ID: "a", Timestamp: 100},
		{ID: "b", Timestamp: 200},
	}

	got := sortOldestFirst(in)

	for i, want := range []string{"a", "b", "c"} {
		if got[i].ID != want {
			t.Errorf("[%d] = %q, want %q", i, got[i].ID, want)
		}
	}
	if in[0].ID != "c" {
		t.Error("input order must not change")
	}
}

func TestProbeWidthHasAMinimum(t *testing.T) {
	if got := probeWidth(0); got != 50 {
		t.Errorf("probeWidth(0) = %d, want 50", got)
	}
	if got := probeWidth(5); got != 50 {
		t.Errorf("probeWidth(5) = %d, want 50", got)
	}
	if got := probeWidth(25); got != 50 {
		t.Errorf("probeWidth(25) = %d, want 50", got)
	}
	if got := probeWidth(75); got != 75 {
		t.Errorf("probeWidth(75) = %d, want 75", got)
	}
	if got := probeWidth(150); got != 100 {
		t.Errorf("probeWidth(150) = %d, want 100", got)
	}
}

func TestDescribeRulesIsReadable(t *testing.T) {
	set, _ := parseConfig([]byte(`rules:
- name: mefamex
  match: { name: "Mefamex" }
  do: [ { type: command, code: "x" } ]
- name: question
  match: { text: "selam" }
  do: [ { type: new_session } ]`))

	got := describeRules(set)
	if !strings.Contains(got, "mefamex[name=Mefamex]") || !strings.Contains(got, "question[text=selam]") {
		t.Errorf("describeRules = %q", got)
	}
	if got := describeRules(ruleSet{}); got != "(no rules)" {
		t.Errorf("empty config = %q", got)
	}
}

func TestFormatWindowShowsBothSides(t *testing.T) {
	in := message{ID: "W", From: "a@s.whatsapp.net", Text: "hello", PushName: "Mefamex"}
	mine := message{ID: "X", From: "a@s.whatsapp.net", Text: "received", IsFromMe: true, Direction: "out"}

	got := formatWindow([]message{in, mine})

	if !strings.Contains(got, "Mefamex: hello") {
		t.Errorf("sender name should appear:\n%s", got)
	}
	if !strings.Contains(got, "me: received") {
		t.Errorf("own message should be 'me':\n%s", got)
	}
}

func TestFormatWindowNotesMedia(t *testing.T) {
	withAudio := message{ID: "W", Text: "", Type: "audio", Media: &media{MimeType: "audio/ogg", FileSize: 2048}}
	withBare := message{ID: "X", Text: "", Type: "sticker", Media: &media{MimeType: "image/webp"}}

	got := formatWindow([]message{withAudio, withBare})

	if !strings.Contains(got, "audio/ogg") {
		t.Errorf("media type should appear:\n%s", got)
	}
	if !strings.Contains(got, "2.0 KB") {
		t.Errorf("file size should appear:\n%s", got)
	}
}

func TestFormatWindowNotesQuoted(t *testing.T) {
	withQuote := message{
		ID:       "W",
		Text:     "Evet katılıyorum",
		PushName: "Mefamex",
		Quoted: &quotedMessage{
			ID:   "Q1",
			Text: "Soru metni burada",
		},
	}

	got := formatWindow([]message{withQuote})
	if !strings.Contains(got, `(yanıt: "Soru metni burada")`) {
		t.Errorf("quoted text should appear:\n%s", got)
	}
}

func TestDecodeEventIgnoresNonMessageEvents(t *testing.T) {
	cases := []string{
		`data: {"event":"connected","timestamp":1}`,
		`data: {"event":"receipt","timestamp":1,"data":{"type":"read"}}`,
		`data: {"event":"message","timestamp":1,"data":{}}`,
		`: comment`,
		``,
	}

	for _, line := range cases {
		if _, use, err := decodeEvent(line); use || err != nil {
			t.Errorf("%q -> use=%v err=%v, both should be false", line, use, err)
		}
	}
}

func TestDecodeEventReadsMessagePayload(t *testing.T) {
	line := `data: {"event":"message","timestamp":1,"data":{"id":"W1","from":"a@lid","push_name":"Mefamex","text":"hello"}}`

	msg, use, err := decodeEvent(line)
	if err != nil || !use {
		t.Fatalf("use=%v err=%v", use, err)
	}
	if msg.ID != "W1" || msg.PushName != "Mefamex" || msg.Text != "hello" {
		t.Errorf("msg = %+v", msg)
	}
}

func TestDecodeEventReportsBrokenJSON(t *testing.T) {
	_, use, err := decodeEvent(`data: {"event":`)
	if err == nil {
		t.Fatal("broken line should error")
	}
	if use {
		t.Error("broken line should not be processed")
	}
}

func TestDecodeMessagesReadsBridgeResponse(t *testing.T) {
	body := []byte(`{"count":2,"jid":"a@lid","messages":[
		{"id":"W1","text":"one","direction":"in"},
		{"id":"W2","text":"two","direction":"out","is_from_me":true}]}`)

	msgs, err := decodeMessages(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].ID != "W2" || !msgs[1].fromMe() {
		t.Errorf("msgs = %+v", msgs)
	}
}

func TestOptionsAreSane(t *testing.T) {
	if defaultActionTimeout <= 0 || defaultActionTimeout > 10*time.Minute {
		t.Errorf("default action timeout = %s", defaultActionTimeout)
	}
}

func TestNormalizeArgsAcceptsBothSyntaxes(t *testing.T) {
	got := normalizeArgs([]string{"--config:/a/b.yaml", "--history=3", "--dry-run", "rest"})

	want := []string{"--config=/a/b.yaml", "--history=3", "--dry-run", "rest"}
	if len(got) != len(want) {
		t.Fatalf("normalizeArgs = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// MEVZU Aşama 3 (ADIM 1, 2026-10-06): rules() her tetiklemede config'i
// yeniden okuduğu için `zamanlanmis_mesaj.acik` değişimi restart'sız sonraki
// rules() çağrısına yansımalıdır.
func TestEngineRefreshesZamanlanmisAcikWithoutRestart(t *testing.T) {
	path := writeRules(t, `zamanlanmis_mesaj: { acik: false }
rules:
- name: r
  match: { everyone: true }
  do: [ { type: command, code: "true" } ]`)

	e := newEngine(path, testOptions())
	e.rules()
	if e.opts.zamanlanmisAcik {
		t.Fatal("acik: false → zamanlanmisAcik false olmalı")
	}

	if err := os.WriteFile(path, []byte("zamanlanmis_mesaj: { acik: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.rules()
	if !e.opts.zamanlanmisAcik {
		t.Fatal("acik: true → rules() sonrası zamanlanmisAcik true olmalı (restart'sız)")
	}

	if err := os.WriteFile(path, []byte("zamanlanmis_mesaj: { acik: false }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.rules()
	if e.opts.zamanlanmisAcik {
		t.Fatal("geri kapama yansımadı")
	}
}

// MEVZU Aşama 3 (ADIM 3, 2026-10-06): tarayıcı goroutine'i bayrağı kapı
// getter'ından okur — rules() tazelediği için /zamanla aç|kapat restart'sız
// yansımalı.
func TestEngineZamanlanmisAcikDurumGetter(t *testing.T) {
	path := writeRules(t, `zamanlanmis_mesaj: { acik: false }
rules:
- name: r
  match: { everyone: true }
  do: [ { type: command, code: "true" } ]`)

	e := newEngine(path, testOptions())
	e.rules()
	if e.zamanlanmisAcikDurum() {
		t.Fatal("başlangıç kapalı olmalı")
	}

	if err := os.WriteFile(path, []byte("zamanlanmis_mesaj: { acik: true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.rules()
	if !e.zamanlanmisAcikDurum() {
		t.Fatal("/zamanla aç sonrası getter true dönmeli (restart'sız)")
	}

	// Eş zamanlı erişim: getter rules() yazarken okur — kilitli, race olmaz.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.zamanlanmisAcikDurum()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = e.rules()
		}
	}()
	wg.Wait()
}
