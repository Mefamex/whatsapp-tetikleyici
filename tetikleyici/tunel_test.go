package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTunelDurumuAl(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TETIKLEYICI_STATE_DIR", tmpDir)

	// Dosya yokken normal dönmeli
	mod, sid := tunelDurumuAl("12345678901234@lid")
	if mod != "normal" || sid != "" {
		t.Fatalf("beklenen normal/boş, alınan: %s/%s", mod, sid)
	}

	// Tünel dosyası yaz
	durumlar := map[string]tunelKaydi{
		"12345678901234@lid": {
			Mod:          "session",
			SessionID:    "ses_custom123",
			SessionTitle: "Özel Oturum",
			CWD:          "/tmp",
		},
		"905551234567@s.whatsapp.net": {
			Mod: "cmdhub",
			CWD: "/tmp",
		},
	}
	data, _ := json.Marshal(durumlar)
	os.WriteFile(filepath.Join(tmpDir, "tunel_durumu.json"), data, 0644)

	// LID eşleşmesi
	mod, sid = tunelDurumuAl("12345678901234@lid")
	if mod != "session" || sid != "ses_custom123" {
		t.Errorf("LID eşleşmedi: got mod=%s sid=%s", mod, sid)
	}

	// Numara eşleşmesi (normalize)
	mod, _ = tunelDurumuAl("905551234567")
	if mod != "cmdhub" {
		t.Errorf("Numara eşleşmedi: got mod=%s", mod)
	}
}

func TestYonlendirCmdhubModundaKomutaGider(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TETIKLEYICI_STATE_DIR", tmpDir)

	durumlar := map[string]tunelKaydi{
		"user1@s.whatsapp.net": {
			Mod: "cmdhub",
		},
	}
	data, _ := json.Marshal(durumlar)
	os.WriteFile(filepath.Join(tmpDir, "tunel_durumu.json"), data, 0644)

	act := tamYetenekliEylem()

	// Normal metin, cmdhub modunda olduğu için cmd hattına gitmeli
	y, ok := act.yonlendir(message{
		From: "user1@s.whatsapp.net",
		Type: "text",
		Text: "uptime",
	})
	if !ok {
		t.Fatal("yönlendirme başarılı olmalı")
	}
	if y.ad != "cmdhub hattı" || y.tip != "command" {
		t.Errorf("cmdhub hattı bekleniyordu: got ad=%s tip=%s", y.ad, y.tip)
	}
}
