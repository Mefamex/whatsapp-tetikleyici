package main

import (
	"strings"
	"testing"
)

func mefamexMessage() message {
	return message{
		ID:        "W1",
		From:      "12345678901234@lid",
		Sender:    "12345678901234@lid",
		Text:      "Müfredat nedir?",
		PushName:  "Mefamex",
		Direction: "in",
	}
}

func TestConditionWithoutAnyFieldMatchesEverything(t *testing.T) {
	if !(condition{}).matches(mefamexMessage()) {
		t.Error("empty condition should match every message")
	}
	if !(condition{Everyone: true}).matches(mefamexMessage()) {
		t.Error("everyone:true should match every message")
	}
}

func TestConditionNeverMatchesOwnMessages(t *testing.T) {
	m := mefamexMessage()
	m.IsFromMe = true
	m.Direction = "out"

	if (condition{}).matches(m) {
		t.Error("own messages should never match")
	}
}

func TestConditionMatchesNumberInEitherJIDField(t *testing.T) {
	m := message{
		ID:        "W1",
		From:      "905550000001@s.whatsapp.net",
		Sender:    "12345678901234@lid",
		Text:      "selam",
		Direction: "in",
	}

	for _, number := range []string{"905550000001", "12345678901234", "905550000001@s.whatsapp.net"} {
		if !(condition{Numbers: stringList{number}}).matches(m) {
			t.Errorf("number %q should match in From or Sender", number)
		}
	}
}

func TestConditionNumbersAreOredWithinField(t *testing.T) {
	if !(condition{Numbers: stringList{"111111111", "12345678901234"}}).matches(mefamexMessage()) {
		t.Error("one of the listed numbers should match")
	}
}

func TestConditionNameAndTextAreCaseInsensitive(t *testing.T) {
	m := mefamexMessage()

	if !(condition{Names: stringList{"mefamex"}}).matches(m) {
		t.Error("name should be case-insensitive")
	}
	if !(condition{Text: "MÜFREDAT"}).matches(m) {
		t.Error("text should be case-insensitive")
	}
}

func TestConditionFieldsAreANDed(t *testing.T) {
	m := mefamexMessage()

	if !(condition{Names: stringList{"Mefamex"}, Text: "müfredat"}).matches(m) {
		t.Error("both should match")
	}
	if (condition{Names: stringList{"Mefamex"}, Text: "hava"}).matches(m) {
		t.Error("one fails should not match")
	}
}

func TestConditionGroupsOnly(t *testing.T) {
	m := mefamexMessage()
	if (condition{GroupsOnly: true}).matches(m) {
		t.Error("group condition should not match individual chat")
	}

	m.IsGroup = true
	if !(condition{GroupsOnly: true}).matches(m) {
		t.Error("group chat should match")
	}
}

func TestConditionDescribe(t *testing.T) {
	got := condition{Names: stringList{"Yusuf"}, Text: "selam"}.describe()
	for _, want := range []string{"name=Yusuf", "text=selam"} {
		if !strings.Contains(got, want) {
			t.Errorf("describe = %q, should contain %q", got, want)
		}
	}
	if got := (condition{}).describe(); got != "everyone" {
		t.Errorf("empty condition should be 'everyone', got %q", got)
	}
}

func TestMatchingRulesReturnsEveryHitInConfigOrder(t *testing.T) {
	set, err := parseConfig([]byte(`rules:
- name: mefamex
  match: { name: "Mefamex" }
  do: [ { type: command, code: "a" } ]
- name: yusuf
  match: { name: "Yusuf" }
  do: [ { type: command, code: "b" } ]
- name: everyone
  match: { everyone: true }
  do: [ { type: command, code: "c" } ]`))
	if err != nil {
		t.Fatal(err)
	}

	hits := matchingRules(set, mefamexMessage())
	if len(hits) != 2 {
		t.Fatalf("matching = %d, want 2", len(hits))
	}
	if hits[0].Name != "mefamex" || hits[1].Name != "everyone" {
		t.Errorf("config order must be preserved, got %s,%s", hits[0].Name, hits[1].Name)
	}
}

func TestMatchingRulesIsEmptyWhenNothingHolds(t *testing.T) {
	set, _ := parseConfig([]byte(`rules:
- name: yusuf
  match: { name: "Yusuf" }
  do: [ { type: command, code: "a" } ]`))

	if hits := matchingRules(set, mefamexMessage()); len(hits) != 0 {
		t.Errorf("should be empty, got %d", len(hits))
	}
}

func TestSeedJIDsCoversBothFormsAndDeduplicates(t *testing.T) {
	set, _ := parseConfig([]byte(`rules:
- name: a
  match: { number: "905550000001" }
  do: [ { type: command, code: "x" } ]
- name: b
  match: { number: "905550000001" }
  do: [ { type: command, code: "y" } ]
- name: c
  match: { number: "905550000005@s.whatsapp.net" }
  do: [ { type: command, code: "z" } ]`))

	got := seedJIDs(set)
	want := []string{"905550000001@s.whatsapp.net", "905550000005@s.whatsapp.net"}

	if len(got) != len(want) {
		t.Fatalf("seedJIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("seedJIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSeedJIDsIsEmptyWithoutNumberRules(t *testing.T) {
	set, _ := parseConfig([]byte(`rules:
- name: everyone
  match: { everyone: true }
  do: [ { type: command, code: "x" } ]`))

	if got := seedJIDs(set); len(got) != 0 {
		t.Errorf("no number rules -> seed should be empty, got %v", got)
	}
}

func TestIdentityTriesNumberFirstThenFallsBackToName(t *testing.T) {
	m := mefamexMessage()
	both := condition{Numbers: stringList{"905550000001"}, Names: stringList{"Mefamex"}}

	got := both.resolve(m)
	if !got.matched {
		t.Fatal("number fails but name matches -> should match")
	}
	if got.via != "name" {
		t.Errorf("via = %q, want name", got.via)
	}
	if got.detail != "Mefamex" {
		t.Errorf("detail = %q", got.detail)
	}
	if !both.matches(m) {
		t.Error("matches should be true")
	}
}

func TestIdentityPrefersTheNumberWhenItMatches(t *testing.T) {
	m := message{
		ID: "W", From: "905550000001@s.whatsapp.net", Sender: "12345678901234@lid",
		PushName: "Mefamex", Direction: "in",
	}

	got := (condition{Numbers: stringList{"905550000001"}, Names: stringList{"Mefamex"}}).resolve(m)

	if got.via != "number" {
		t.Errorf("via = %q, want number (priority)", got.via)
	}
}

func TestIdentityFailsWhenNeitherNumberNorNameMatches(t *testing.T) {
	both := condition{Numbers: stringList{"905999999999"}, Names: stringList{"Yusuf"}}

	if got := both.resolve(mefamexMessage()); got.matched {
		t.Errorf("neither number nor name matches: %+v", got)
	}
	if both.matches(mefamexMessage()) {
		t.Error("matches should be false")
	}
}

func TestIdentityTriesEveryListedNumber(t *testing.T) {
	got := (condition{Numbers: stringList{"111111111", "12345678901234"}}).resolve(mefamexMessage())

	if !got.matched || got.detail != "12345678901234" {
		t.Errorf("resolve = %+v", got)
	}
}

func TestIdentityWithNoFieldsMatchesEverything(t *testing.T) {
	got := (condition{}).resolve(mefamexMessage())
	if !got.matched || got.via != "everyone" {
		t.Errorf("resolve = %+v", got)
	}
}

func TestIdentityDetailLineExplainsTheLIDFallback(t *testing.T) {
	line := (condition{Numbers: stringList{"905550000001"}, Names: stringList{"Mefamex"}}).
		resolve(mefamexMessage()).detailLine()

	if !strings.Contains(line, "name=Mefamex") {
		t.Errorf("detailLine = %q", line)
	}
	if !strings.Contains(line, "number") {
		t.Errorf("should mention number not in payload: %q", line)
	}
}

func TestIdentityDetailLineForNumber(t *testing.T) {
	line := (condition{Numbers: stringList{"12345678901234"}}).resolve(mefamexMessage()).detailLine()
	if line != "number=12345678901234" {
		t.Errorf("detailLine = %q", line)
	}
}

func TestConditionDescribeMarksTheNameAsFallback(t *testing.T) {
	got := (condition{Numbers: stringList{"905550000001"}, Names: stringList{"Mefamex"}}).describe()
	if !strings.Contains(got, "name=Mefamex (fallback)") {
		t.Errorf("describe = %q, name should be marked as fallback", got)
	}

	only := (condition{Names: stringList{"Mefamex"}}).describe()
	if strings.Contains(only, "fallback") {
		t.Errorf("only name -> 'fallback' should not appear: %q", only)
	}
}

func TestConditionTextMatchingRespectsTurkishCharacters(t *testing.T) {
	m := mefamexMessage()

	if (condition{Text: "mufredat"}).matches(m) {
		t.Error("plain u should not match ü")
	}
	if !(condition{Text: "MÜFREDAT"}).matches(m) {
		t.Error("uppercase with ü should match")
	}
}

func TestConditionNotTextExcludesMatchingMessages(t *testing.T) {
	ping := mefamexMessage()
	ping.Text = "PING"

	// Hariç tutma büyük/küçük harf duyarsızdır.
	if (condition{Names: stringList{"Mefamex"}, NotText: "ping"}).matches(ping) {
		t.Error("not_text should exclude a message containing it")
	}

	other := mefamexMessage()
	if !(condition{Names: stringList{"Mefamex"}, NotText: "ping"}).matches(other) {
		t.Error("not_text should not exclude other messages")
	}

	// Text + NotText birlikte: ikisi de tutarsa kural tutmaz.
	if (condition{Text: "müfredat", NotText: "nedir"}).matches(mefamexMessage()) {
		t.Error("text matches but not_text also matches -> should not match")
	}
	if !(condition{Text: "müfredat", NotText: "hava"}).matches(mefamexMessage()) {
		t.Error("text matches and not_text absent -> should match")
	}
}

// Sticker, reaksiyon ve boş payload'da metin yoktur. Metin bekleyen kural
// bunları TUTMAMALI — yoksa ajan boş pencereyle tetiklenip penceredeki
// başka mesajı işliyor. Ölçüm: export/1-mefamex 20:05.
func TestBosMetinMetinBekleyenKuraliTutmaz(t *testing.T) {
	alan := func() condition {
		return condition{Text: "durum"}
	}

	mesajlar := map[string]message{
		"sticker":   {Type: "sticker", Text: ""},
		"reaksiyon": {Type: "reaction", Text: ""},
		"bos":       {Type: "text", Text: ""},
		"ses":       {Type: "audio", Text: ""},
		"yalnizca":  {Type: "text", Text: "   "},
	}

	for ad, m := range mesajlar {
		if alan().contentHolds(m) {
			t.Errorf("%s: boş metinli mesaj kuralı tuttu, tutmamalıydı", ad)
		}
	}

	// Gerçekten eşleşen mesaj yine tutmalı.
	if !alan().contentHolds(message{Type: "text", Text: "durum nedir"}) {
		t.Error("metin eşleşmesi tutmadı, tutmalıydı")
	}
}

// everyone + komut gibi metin ARAMAYAN kural boş mesajı yakalayabilmeli:
// "her mesajı arşivle" işi boş olsa da anlamlı.
func TestBosMetinKuraliMetinAramayanKuraliEngellemez(t *testing.T) {
	alan := condition{Everyone: true}

	if !alan.contentHolds(message{Type: "sticker", Text: ""}) {
		t.Error("koşulsuz kural boş mesajı eledi, elemesiydi")
	}
}

// not_text yazan kural da metin bekler: "ping" dışındaki her şeyi oturuma
// gönderen kural, sticker'ı oturuma göndermemeli.
func TestBosMetinNotTextKuraliniEler(t *testing.T) {
	alan := condition{NotText: "ping"}

	if alan.contentHolds(message{Type: "sticker", Text: ""}) {
		t.Error("not_text kuralı boş metinli sticker'ı tuttu, tutmamalıydı")
	}
	if !alan.contentHolds(message{Type: "text", Text: "merhaba"}) {
		t.Error("not_text kuralı gerçek mesajı eledi, elemesiydi")
	}
}

// type kısıtı "metin bekliyor" sayılmaz: sesli notun metni YOKTUR ve
// `type: audio` kuralı onu yakalamak için vardır. İlk yazımda koruma
// tip koşulunu da sayıyordu, sesli not hattını sessizce öldürüyordu —
// type_test.go'daki 3 test ve shipped_config_test kırıldı.
func TestTipKisitliKuralMetinsizMesajiYakalar(t *testing.T) {
	ses := message{Type: "audio", Text: ""}
	sesKurali := condition{Types: []string{"audio"}}
	metinKurali := condition{Types: []string{"text"}}

	if !sesKurali.contentHolds(ses) {
		t.Error("type: audio kuralı metinsiz sesli notu yakalamadı")
	}
	if metinKurali.contentHolds(ses) {
		t.Error("type: text kuralı sesli notu yakalamamalıydı")
	}

	// not_text'li oturum kuralı da metin bekler: sticker elenmeli.
	oturum := condition{Types: []string{"text"}, NotText: "ping"}
	sticker := message{Type: "sticker", Text: ""}

	if oturum.contentHolds(sticker) {
		t.Error("type: text + not_text kuralı boş metinli sticker'ı tuttu")
	}
}
