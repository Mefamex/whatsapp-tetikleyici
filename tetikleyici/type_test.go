// whatsapp-bridge-project/tetikleyici/type_test.go

package main

import (
	"strings"
	"testing"
)

// voiceMessage, Mefamex'ten gelen bir sesli notu taklit eder.
func voiceMessage() message {
	return message{
		ID:        "V1",
		Type:      "audio",
		From:      "12345678901234@lid",
		Sender:    "12345678901234@lid",
		Text:      "",
		PushName:  "Mefamex",
		Direction: "in",
		Media:     &media{MimeType: "audio/ogg; codecs=opus", FileSize: 99938},
	}
}

func TestConditionTypeMatchesAudio(t *testing.T) {
	c := condition{Names: stringList{"Mefamex"}, Types: stringList{"audio"}}

	if !c.matches(voiceMessage()) {
		t.Error("audio type should match an audio message")
	}
	if c.matches(mefamexMessage()) {
		t.Error("a text message must not match type=audio")
	}
}

func TestConditionTypeIsCaseInsensitiveAndListable(t *testing.T) {
	c := condition{Names: stringList{"Mefamex"}, Types: stringList{"Audio"}}
	if !c.matches(voiceMessage()) {
		t.Error("type matching should ignore case")
	}

	multi := condition{Types: stringList{"image", "video"}}
	if multi.matches(voiceMessage()) {
		t.Error("audio is not in [image, video]")
	}

	// Virgülle ayrılmış tek satır da stringList'in işi; burada yalnız
	// "audio" tek başına ikinci bir aday olsun.
	pair := condition{Types: stringList{"audio", "ptt"}}
	if !pair.matches(voiceMessage()) {
		t.Error("audio is in [audio, ptt]")
	}
}

func TestConditionTypeCombinesWithNumberAndText(t *testing.T) {
	// number + type birlikte AND'lanır: sesli not Mefamex'ten gelmeli.
	c := condition{Numbers: stringList{"905551234567"}, Types: stringList{"audio"}}

	// LID sohbetinde numara payload'da yok → kural tutmamalı.
	if c.matches(voiceMessage()) {
		t.Error("number should not match a LID chat")
	}

	// numara biçiminde açılan sohbet → tutar.
	numeric := message{
		ID: "V2", Type: "audio", From: "905551234567@s.whatsapp.net",
		Sender: "905551234567@s.whatsapp.net", PushName: "Mefamex", Direction: "in",
	}
	if !c.matches(numeric) {
		t.Error("number + type should match a phone-format chat")
	}
}

func TestConditionTypeShowsInDescribe(t *testing.T) {
	got := (condition{Names: stringList{"Mefamex"}, Types: stringList{"audio", "video"}}).describe()
	if !strings.Contains(got, "type=audio|video") {
		t.Errorf("describe = %q, want type=audio|video", got)
	}
}

func TestConditionWithOnlyTypeIsNotEmpty(t *testing.T) {
	// Yalnız "type" yazılmış kural "her mesaj" sanılmamalı.
	if (condition{Types: stringList{"audio"}}).isEmpty() {
		t.Error("type-only condition should not be empty")
	}
	if (condition{Text: "test"}).describe() != "text=test" {
		t.Error("text-only describe changed unexpectedly")
	}
}
