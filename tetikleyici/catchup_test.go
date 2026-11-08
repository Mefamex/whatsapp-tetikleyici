package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCatchUpAtlarBayatMesajlari(t *testing.T) {
	// 1. Sahte köprü sunucusu kur
	simdi := time.Now().Unix()
	bayatMesajZamani := simdi - 1800 // 30 dakika önce
	tazeMesajZamani := simdi - 60    // 1 dakika önce

	// JSON formatında dinamik yanıt oluştur
	jsonBody := `{
		"count": 2,
		"jid": "12345678901234@lid",
		"messages": [
			{
				"id": "MSG_BAYAT_001",
				"type": "text",
				"direction": "in",
				"from": "12345678901234@lid",
				"timestamp": ` + timeStr(bayatMesajZamani) + `,
				"text": "eski selam",
				"is_from_me": false
			},
			{
				"id": "MSG_TAZE_002",
				"type": "text",
				"direction": "in",
				"from": "12345678901234@lid",
				"timestamp": ` + timeStr(tazeMesajZamani) + `,
				"text": "yeni selam",
				"is_from_me": false
			}
		]
	}`

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(jsonBody))
	}))
	defer ts2.Close()

	client := newBridgeClient(ts2.URL, 5*time.Second)

	// Geçici config
	dir := t.TempDir()
	configYolu := filepath.Join(dir, "tetikleyici.yaml")
	cfg := `
catchup_max_age: 10m
rules:
  - name: test
    match:
      number: ["12345678901234@lid"]
    do:
      - type: command
        code: "echo ok"
`
	if err := os.WriteFile(configYolu, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	opts := options{
		probeTimeout:  5 * time.Second,
		catchUpMaxAge: 10 * time.Minute,
	}
	eng := newEngine(configYolu, opts)

	seenFile := filepath.Join(dir, "seen.txt")
	seen := newSeenStore(seenFile)

	err := catchUp(context.Background(), eng, client, seen, []string{"12345678901234@lid"})
	if err != nil {
		t.Fatalf("catchUp hatası: %v", err)
	}

	// Bayat mesaj seenStore'a işlenmiş olmalı
	if !seen.alreadyHandled("MSG_BAYAT_001") {
		t.Errorf("MSG_BAYAT_001 atlanıp görüldü olarak işaretlenmeliydi")
	}

	// Taze mesaj da işlenip seenStore'a geçmiş olmalı
	if !seen.alreadyHandled("MSG_TAZE_002") {
		t.Errorf("MSG_TAZE_002 işlenmeliydi")
	}
}

func timeStr(n int64) string {
	return strconv.FormatInt(n, 10)
}
