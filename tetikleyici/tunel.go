package main

import (
	"encoding/json"
	"os"
	"strings"
)

type tunelKaydi struct {
	Mod          string `json:"mod"`
	SessionID    string `json:"session_id"`
	SessionTitle string `json:"session_title"`
	CWD          string `json:"cwd"`
}

// tunelDurumuAl, verilen JID için tünel modunu ve sessionID'sini döndürür.
// Dosya: defaultState("tunel_durumu.json")
func tunelDurumuAl(from string) (mod string, sessionID string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "normal", ""
	}

	dosya := defaultState("tunel_durumu.json")
	data, err := os.ReadFile(dosya)
	if err != nil {
		return "normal", ""
	}

	var durumlar map[string]tunelKaydi
	if err := json.Unmarshal(data, &durumlar); err != nil {
		return "normal", ""
	}

	// 1. Tam anahtar eşleşmesi
	if k, varMi := durumlar[from]; varMi {
		if k.Mod != "" {
			return k.Mod, k.SessionID
		}
	}

	// 2. Numara kısmı eşleşmesi (örn. 905550000001@s.whatsapp.net -> 905550000001)
	temizFrom := strings.Split(from, "@")[0]
	for k, v := range durumlar {
		if strings.Split(k, "@")[0] == temizFrom {
			if v.Mod != "" {
				return v.Mod, v.SessionID
			}
		}
	}

	return "normal", ""
}
