// whatsapp-bridge-project/tetikleyici/json.go

package main

import "encoding/json"

// jsonMarshal, tek giriş noktası olsun diye sarmalanmış JSON çeviricidir.
//
// Amaç, action.go'nun içine encoding/json import'u serpmek yerine tek bir
// yerden geçmek: payload üretimi ve mesaj serileştirme aynı yerde durur.
func jsonMarshal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// jsonUnmarshal, jsonMarshal'ın okuma ikizidir. Çözülemezse (yarım yazım,
// elle bozulma) hata döner; çağıran karar verir — bozuk damga dosyasında
// boş başlamak, hiç göndermemekten iyidir.
func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}
