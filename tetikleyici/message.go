// whatsapp-bridge-project/tetikleyici/message.go

// Package main — tetikleyici, WhatsApp köprüsünün HTTP API'sini dinleyip
// mesajları kural dosyasındaki kurallarla eşleştiren bağımsız bir araçtır.
//
// Köprüyle bağı yalnızca HTTP/JSON'dur: bu modül bridge-go'nun paketlerine
// bağlı değildir, dolayısıyla köprü yeniden derlendiğinde burası etkilenmez.
package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// message, köprünün /events ve /messages uçlarından gelen mesajın bu araç
// için gereken alanları. Köprünün kendi struct'ına değil, **yayımladığı JSON
// şekline** bağlıdır; köprü alan eklerse bu araç etkilenmez, alan çıkarırsa
// ilgili alan boş gelir.
type message struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Direction string `json:"direction"`
	From      string `json:"from"`
	Sender    string `json:"sender"`
	Timestamp int64  `json:"timestamp"`
	Text      string `json:"text"`
	IsFromMe  bool   `json:"is_from_me"`
	IsGroup   bool   `json:"is_group"`
	PushName  string         `json:"push_name"`
	Media     *media         `json:"media"`
	Quoted    *quotedMessage `json:"quoted,omitempty"`
}

// quotedMessage, alıntılanan (sağa kaydırılarak yanıtlanan) mesajın özeti.
type quotedMessage struct {
	ID     string `json:"id,omitempty"`
	Sender string `json:"sender,omitempty"`
	Type   string `json:"type,omitempty"`
	Text   string `json:"text,omitempty"`
}

// media, eklerin türünü gösteren üst bilgi.
type media struct {
	MimeType string `json:"mime_type"`
	FileSize uint64 `json:"file_length"`
}

// fromMe, mesajın bizden gelip gelmediğini bildirir. Köprü iki alanda da
// bildirebiliyor; hangisi doğruysa ona bakmak tek kaynaklı kuralı bozmaz.
func (m message) fromMe() bool {
	return m.IsFromMe || m.Direction == "out"
}

// label, log satırında ve komut isteminde kullanılacak okunabilir isim.
func (m message) label() string {
	if name := strings.TrimSpace(m.PushName); name != "" {
		return name
	}
	if m.From != "" {
		return m.From
	}
	return "bilinmiyor"
}

// readableText, mesajın metnini döndürür. Köprü tanımadığı bir tipi
// "[unsupported: X]" olarak saklarsa kullanıcı bunu görür ama mesajın gerçekten
// boş olduğunu sanmaz.
func (m message) readableText() string {
	if m.Type == "poll_vote" && strings.TrimSpace(m.Text) != "" {
		return fmt.Sprintf("[Anket Oyu: %s]", m.Text)
	}
	if text := strings.TrimSpace(m.Text); text != "" {
		return text
	}
	if m.Type == "" {
		return "[okunamayan mesaj]"
	}
	return fmt.Sprintf("[%s tipinde mesaj]", m.Type)
}

// mediaNote, ek varsa kısa bir açıklama döndürür.
func (m message) mediaNote() string {
	if m.Media == nil {
		return ""
	}
	if m.Media.FileSize == 0 {
		return fmt.Sprintf("[ek: %s]", m.Media.MimeType)
	}
	return fmt.Sprintf("[ek: %s, %s]", m.Media.MimeType, humanSize(m.Media.FileSize))
}

// sseEvent, /events akışının bir veri satırının zarfı.
//
// Köprü şu biçimde yayınlıyor (eventbus.Bus.frame):
//
//	data: {"event":"message","timestamp":1791117185,"data":{...mesaj...}}
//
// Bu aracın konusu yalnız "message" olayıdır; receipt / presence /
// history_sync sessizce geçilir.
type sseEvent struct {
	Event     string  `json:"event"`
	Timestamp int64   `json:"timestamp"`
	Data      message `json:"data"`
}

// decodeEvent, SSE akışındaki tek bir satırı çözer.
//
// Dönüş değerleri: mesaj, işlenip işlenmeyeceği, hata. Satır `data:` ile
// başlamıyorsa ya da türü "message" değilse sorun değildir.
func decodeEvent(line string) (message, bool, error) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(line), "data:")
	if !ok {
		return message{}, false, nil
	}

	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[DONE]" {
		return message{}, false, nil
	}

	var event sseEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return message{}, false, fmt.Errorf("event unparsable: %w", err)
	}

	if event.Event != "message" {
		return message{}, false, nil
	}
	if event.Data.ID == "" {
		return message{}, false, nil
	}

	return event.Data, true, nil
}

// decodeMessages, /messages uçlarının yanıtını çözer.
func decodeMessages(body []byte) ([]message, error) {
	var payload struct {
		Count    int       `json:"count"`
		Messages []message `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("mesaj listesi çözümlenemedi: %w", err)
	}
	return payload.Messages, nil
}

// humanSize, baytı okunabilir birime çevirir.
func humanSize(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// truncate, metni tek satırda gösterilecek kadar kısaltır.
//
// Kesme bayt değil, RUNE sınırında yapılır. Bayt sınırında kesmek çok
// baytlı bir karakteri ortasından böler ve loga bozuk UTF-8 yazar
// ("administrator olarak de�"). Günlük satırı ayrıştıran betikler
// (export, grep) bu satırları atlayabiliyor.
func truncate(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return string([]rune(s)[:max])
	}
	// max baytlık ölçü, sonra rune geriye doğru çekilir.
	kirp := s[:max-3]
	for !utf8.ValidString(kirp) && kirp != "" {
		kirp = kirp[:len(kirp)-1]
	}
	return kirp + "..."
}
