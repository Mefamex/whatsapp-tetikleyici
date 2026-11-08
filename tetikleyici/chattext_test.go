// whatsapp-bridge-project/tetikleyici/chattext_test.go

package main

import "testing"

func TestChatTextConvertsBoldToWhatsAppSyntax(t *testing.T) {
	cases := map[string]string{
		`**kalın**`:           `*kalın*`,
		`**a** ve **b**`:      `*a* ve *b*`,
		`***hem***`:           `*hem*`,
		`Bu oturum **"ad"**.`: `Bu oturum *"ad"*.`,
	}

	for in, want := range cases {
		if got := chatText(in); got != want {
			t.Errorf("chatText(%q) = %q, want %q", in, got, want)
		}
	}
}

// WhatsApp *kalın* kullanır; metnin kendisi asteriske değdiğinde
// dönüştürmenin bir şey yapmaması gerekir.
func TestChatTextLeavesPlainTextAlone(t *testing.T) {
	for _, in := range []string{
		"pong",
		"2 * 3 = 6",
		"a*b*c",
		"",
	} {
		if got := chatText(in); got != in {
			t.Errorf("chatText(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestChatTextStripsInlineCodeFences(t *testing.T) {
	cases := map[string]string{
		"ID: `ses_abc`":   "ID: ses_abc",
		"**a** `b` **c**": "*a* b *c*",
	}

	for in, want := range cases {
		if got := chatText(in); got != want {
			t.Errorf("chatText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChatTextRemovesHeadingMarks(t *testing.T) {
	in := "## Özet\n\nBir şey."
	if got := chatText(in); got != "Özet\n\nBir şey." {
		t.Errorf("chatText(%q) = %q", in, got)
	}
}

func TestChatTextCollapsesBlankLineRuns(t *testing.T) {
	in := "bir\n\n\n\niki"
	if got := chatText(in); got != "bir\n\niki" {
		t.Errorf("chatText(%q) = %q, want a single blank line", in, got)
	}
}

func TestChatTextTrimsTrailingSpaces(t *testing.T) {
	in := "satır bir   \nsatır iki\t\n"
	if got := chatText(in); got != "satır bir\nsatır iki" {
		t.Errorf("chatText(%q) = %q", in, got)
	}
}

// Madde işaretleri WhatsApp'ta okunur bir • olur; model her zaman
// yıldızla yazsa bile liste görünür kalmalı.
func TestChatTextNormalisesBullets(t *testing.T) {
	in := "- bir\n- iki"
	if got := chatText(in); got != "• bir\n• iki" {
		t.Errorf("chatText(%q) = %q", in, got)
	}
}

// Asıl gözlemlenen durum: model yanıtı hem kalın hem kod içeriyordu ve
// ikisi de ham işaret olarak gidiyordu.
func TestChatTextHandlesTheObservedSessionReplies(t *testing.T) {
	cases := map[string]string{
		`Bu oturum artık **"Mefamex WhatsApp"** isminde.`:                 `Bu oturum artık *"Mefamex WhatsApp"* isminde.`,
		"Oturum ID: `ses_ef8590662ffeRSLXLL2R8VhJfM`":                     "Oturum ID: ses_ef8590662ffeRSLXLL2R8VhJfM",
		"Evet, biliyorum — bu bilgisayarın ve bu oturumun sahibi sensin.": "Evet, biliyorum — bu bilgisayarın ve bu oturumun sahibi sensin.",
	}

	for in, want := range cases {
		if got := chatText(in); got != want {
			t.Errorf("chatText(%q)\n = %q\nwant %q", in, got, want)
		}
	}
}
