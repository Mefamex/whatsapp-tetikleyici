// whatsapp-bridge-project/tetikleyici/bridge.go

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// bridgeClient, köprünün HTTP API'siyle konuşur. Tek bağımlılığı budur.
type bridgeClient struct {
	base    string
	http    *http.Client
	stream  *http.Client
	timeout time.Duration
}

// newBridgeClient, verilen köprü adresine bağlanacak istemci kurar.
//
// normal istekler için kısa zaman aşımı, SSE için zaman aşımısız istemci
// kullanılır: akış 30 saniyede bir kopacağı için normal zaman aşımı her
// bağlantıyı öldürürdü.
func newBridgeClient(base string, timeout time.Duration) *bridgeClient {
	base = strings.TrimRight(base, "/")

	stream := *http.DefaultClient
	stream.Timeout = 0

	return &bridgeClient{
		base:    base,
		http:    &http.Client{Timeout: timeout},
		stream:  &stream,
		timeout: timeout,
	}
}

// streamEvents, /events uçtan okur ve her mesaj için onMessage çağırır.
//
// Bağlantı koparsa hata döner; çağıran taraf yeniden bağlanmayı üstlenir.
func (c *bridgeClient) streamEvents(ctx context.Context, onMessage func(message)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/events", nil)
	if err != nil {
		return fmt.Errorf("istek kurulamadı: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.stream.Do(req)
	if err != nil {
		return fmt.Errorf("akış açılamadı: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("akış reddedildi: HTTP %d", resp.StatusCode)
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return io.EOF
			}

			msg, use, err := decodeEvent(line)
			if err != nil {
				// Tek bir bozuk satır akışı kesmemeli.
				continue
			}
			if use {
				onMessage(msg)
			}
		}
	}
}

// history, bir sohbetin son mesajlarını getirir.
//
// count=1 isteği köprüde yalnız bellekten karşılanır; geniş pencere istenirse
// köprü WhatsApp'a sorar ve yanıtın gelmesi on saniye kadar sürebilir. Bu
// yüzden çağıran taraf kendi zaman aşımını yönetmelidir.
// history, bir sohbetin son mesajlarını okur.
//
// HIZLI istekler (topupKapali=true) köprüye "WhatsApp'a gitme, yerel tamponu
// ver" der. NEDEN: varsayılan davranış, tampon yetmediğinde WhatsApp'a gidip
// 10 saniye bekliyor — ve çağıranların bütçesi 8 saniye. ÖLÇÜLDÜ (2026-10-06):
// /messages?count=12 tam 10,12 sn sürüyordu, tetikleyicinin çift gönderim
// denetimi bu yüzden HER SEFERİNDE "geçmiş alınamadı" deyip korumayı sessizce
// atlıyordu.
//
// topupKapali=false: geçmişin TAMAMINI isteyenler (ajanın bağlam penceresi).
// Orada beklemek değer: eksik geçmiş ajanın cevabını bozar.
func (c *bridgeClient) history(ctx context.Context, jid string, count int) ([]message, error) {
	return c.historyTopup(ctx, jid, count, false)
}

// historyHizli, geçmişin tamamını beklemeden okur. Çift gönderim denetimi
// bunu kullanır: soru "son 2 dakikada bu metin gitti mi", tam geçmiş değil.
func (c *bridgeClient) historyHizli(ctx context.Context, jid string, count int) ([]message, error) {
	return c.historyTopup(ctx, jid, count, true)
}

func (c *bridgeClient) historyTopup(ctx context.Context, jid string, count int, topupKapali bool) ([]message, error) {
	url := fmt.Sprintf("%s/messages/%s?count=%d", c.base, jid, count)
	if topupKapali {
		url += "&topup=off"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("istek kurulamadı: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geçmiş alınamadı: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geçmiş reddedildi: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("geçmiş okunamadı: %w", err)
	}

	msgs, err := decodeMessages(body)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("geçmiş boş")
	}
	return msgs, nil
}

// sendText, tetikleyen sohbete bir metin mesajı gönderir.
//
// LID kimlikleri olduğu gibi iletilir: köprü "@" içeren hedeflere dokunmaz,
// yani 12345678901234@lid doğrudan çalışır. Numara biçimi de kabul edilir.
func (c *bridgeClient) sendText(ctx context.Context, to, text string) error {
	body, err := jsonMarshal(map[string]string{"to": to, "text": text})
	if err != nil {
		return fmt.Errorf("gönderim gövdesi hazırlanamadı: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/send", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("istek kurulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("köprü reddetti (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

// markRead, mesajları okundu olarak işaretler (mavi tik).
func (c *bridgeClient) markRead(ctx context.Context, jid string, messageIDs []string) error {
	body, err := jsonMarshal(map[string]any{"jid": jid, "message_ids": messageIDs})
	if err != nil {
		return fmt.Errorf("mark-read gövdesi hazırlanamadı: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/mark-read", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("istek kurulamadı: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gönderilemedi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("köprü reddetti (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

// health, köprünün bağlı olup olmadığını bildirir.
func (c *bridgeClient) health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("köprüye ulaşılamadı: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("köprü yanıt vermedi: HTTP %d", resp.StatusCode)
	}
	return nil
}

// formatWindow, mesajları komuta verilecek okunabilir metne çevirir.
func formatWindow(msgs []message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.fromMe() {
			b.WriteString("me: ")
		} else {
			b.WriteString(m.label() + ": ")
		}
		b.WriteString(m.readableText())
		if note := m.mediaNote(); note != "" {
			b.WriteString(" " + note)
		}
		if m.Quoted != nil && strings.TrimSpace(m.Quoted.Text) != "" {
			b.WriteString(fmt.Sprintf(" (yanıt: %q)", truncate(m.Quoted.Text, 60)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// formatClock, unix zamanı okunabilir saate çevirir.
func formatClock(ts int64) string {
	if ts == 0 {
		return "--:--:--"
	}
	return time.Unix(ts, 0).Format("15:04:05")
}

// parseCount, pozitif sayı çevirir; geçersizse fallback döner.
func parseCount(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
