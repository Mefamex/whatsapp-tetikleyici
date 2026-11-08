package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Süre çözümlemesi: bozuk metin sessizce varsayılana düşmeli, çökertmemeli.
func TestParseSureFallsBackOnGarbage(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"15m", 15 * time.Minute},
		{"1m", time.Minute},
		{"", defaultHeartbeatEvery},
		{"15", defaultHeartbeatEvery},  // birimsiz → reddedilir
		{"-5m", defaultHeartbeatEvery}, // negatif → reddedilir
		{"abc", defaultHeartbeatEvery},
	}

	for _, tc := range cases {
		if got := parseSure(tc.in, defaultHeartbeatEvery); got != tc.want {
			t.Errorf("parseSure(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Yapılandırılmamışsa koşucu üretilmemeli: session olmadan kime gidecek?
func TestHeartbeatDisabledWithoutSession(t *testing.T) {
	if (heartbeat{}).enabled() {
		t.Error("boş heartbeat etkin olmamalı")
	}
	if (heartbeat{Mesaj: "metin"}).enabled() {
		t.Error("session yoksa etkin olmamalı")
	}
	if (heartbeat{SessionID: "ses_x"}).enabled() {
		t.Error("mesaj yoksa etkin olmamalı")
	}
	if !(heartbeat{SessionID: "ses_x", Mesaj: "metin"}).enabled() {
		t.Error("session + mesaj varsa etkin olmalı")
	}
}

// newHeartbeat, yapılandırılmamış config'de nil döner.
func TestNewHeartbeatReturnsNilWhenUnconfigured(t *testing.T) {
	if hb := newHeartbeat(ruleSet{}, nil, options{}); hb != nil {
		t.Error("yapılandırılmamış config için nil bekleniyor")
	}
}

// Süreler config'den okunmalı.
func TestNewHeartbeatReadsDurations(t *testing.T) {
	set := ruleSet{Heartbeat: heartbeat{
		SessionID: "ses_abc",
		Mesaj:     "durağan",
		Idle:      "20m",
		Every:     "30s",
	}}

	hb := newHeartbeat(set, nil, options{})
	if hb == nil {
		t.Fatal("koşucu üretilmeli")
	}
	if hb.idle != 20*time.Minute {
		t.Errorf("idle = %v, want 20m", hb.idle)
	}
	if hb.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", hb.interval)
	}
}

// Sessizlik hesabı: eşiği aşan hareket sessiz sayılır, taze hareket sayılmaz.
func TestActivityIsIdle(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	idle := 15 * time.Minute

	cases := []struct {
		name string
		at   int64
		want bool
	}{
		{"tam eşik", now.Add(-15 * time.Minute).Unix(), true},
		{"eşiği aştı", now.Add(-20 * time.Minute).Unix(), true},
		{"taze", now.Add(-2 * time.Minute).Unix(), false},
		{"şimdi", now.Unix(), false},
		// Mesaj yoksa uyarı üretmek istemeyiz: kullanıcı henüz konuşmamış
		// olabilir. Yanlış tetik, doğru tetikten daha kötüdür.
		{"mesaj yok", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (activity{At: tc.at}).isIdle(idle, now); got != tc.want {
				t.Errorf("isIdle = %v, want %v", got, tc.want)
			}
		})
	}
}

// Gerçek HTTP yanıtından son hareket okunmalı; en yeni mesaj seçilmeli.
func TestLastActivityPicksNewestMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// store newest-first döndürür; en yeni zaman 1700 olmalı
		_, _ = w.Write([]byte(`{"count":2,"messages":[
			{"id":"A","timestamp":1700,"text":"yeni"},
			{"id":"B","timestamp":1600,"text":"eski"}]}`))
	}))
	defer srv.Close()

	c := newBridgeClient(srv.URL, 2*time.Second)

	got, err := c.lastActivity(context.Background(), "1@lid")
	if err != nil {
		t.Fatal(err)
	}
	if got.At != 1700 {
		t.Errorf("At = %d, want 1700", got.At)
	}
}

// Boş sohbet hata vermemeli: "sessiz" değil, "henüz konuşulmamış" demektir.
func TestLastActivityEmptyChatIsNotIdle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"count":0,"messages":[]}`))
	}))
	defer srv.Close()

	c := newBridgeClient(srv.URL, 2*time.Second)

	got, err := c.lastActivity(context.Background(), "1@lid")
	if err != nil {
		t.Fatal(err)
	}
	if got.At != 0 {
		t.Errorf("At = %d, want 0", got.At)
	}
	if got.isIdle(time.Minute, time.Now()) {
		t.Error("mesaj yoksa sessiz sayılmamalı")
	}
}

// Köprü düşükse tur hata verir ama diğer sohbetler yine kontrol edilir.
func TestLastActivityPropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Not connected", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newBridgeClient(srv.URL, 2*time.Second)

	if _, err := c.lastActivity(context.Background(), "1@lid"); err == nil {
		t.Error("köprü hatası yutulmamalı")
	}
}

// opencode yolu her turda tazelenmeli: startup kopyası boşken bile nudge
// çalışabilmeli (2026-10-07: 4 tur "tanımlı değil" diye düştü).
func TestTazeleOptsOpencodeYolunuDoldurur(t *testing.T) {
	hb := &heartbeatRunner{opts: options{}}
	hb.tazeleOpts(ruleSet{OpenCode: "~/.opencode/bin/opencode"})
	if hb.opts.opencode == "" {
		t.Error("opencode yolu tazelenmedi")
	}
}

// Boş OpenCode mevcut değeri silmemeli.
func TestTazeleOptsBosDegeriKorumaz(t *testing.T) {
	hb := &heartbeatRunner{opts: options{opencode: "/usr/bin/opencode"}}
	hb.tazeleOpts(ruleSet{})
	if hb.opts.opencode != "/usr/bin/opencode" {
		t.Errorf("opencode = %q, want korunmalı", hb.opts.opencode)
	}
}
