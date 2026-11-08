// whatsapp-bridge-project/tetikleyici/heartbeat.go

package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// heartbeat, sessiz kalan sohbeti belirli aralıklarla uyarır.
//
// Neden var: "admin konuşmuyor mu" sorusu, tetikleyicinin kendisinden sorulacak
// şekilde bırakılmıştı. Bu, hat sağlığı testi **değildir** — onun işi `ping`
// kuralıdır. Buradaki ölçüt farklı: sohbette **trafik** var mı, yok mu.
//
// Döngü riski ve nasıl kırıldığı: uyarı bir kez gider, ajan cevap verir, o
// cevap trafik sayılır. Sessizlik eşiği dolmadığı için ikinci turda tekrar
// tetiklenmez. Bu yüzden "her turda" uyarı gönderen bir döngüye dönüşmez.
//
// Hata politikası: bir turda hata olursa tur atlanır ve günlüğe yazılır. Kalp
// atışı, hatalı da olsa sürekli çalışmaya devam eder — durursa kullanıcıya
// sessizce kaybolur, ki bu tam olarak kaçınmak istediğimiz şey.
type heartbeatRunner struct {
	client   *bridgeClient
	opts     options
	interval time.Duration
	idle     time.Duration
}

// newHeartbeat, yapılandırılmışsa çalışan bir koşucu döndürür.
//
// Yapılandırılmamışsa (nil) döner; çağıran taraf bunu kontrol eder. Süreler
// çözülemiyorsa varsayılana düşer ve günlüğe yazılır — çalışmayı durdurmak,
// kullanıcıyı eski bir ayarla bırakmaktan iyidir.
func newHeartbeat(set ruleSet, client *bridgeClient, opts options) *heartbeatRunner {
	h := set.Heartbeat
	if !h.enabled() {
		return nil
	}

	interval := parseSure(h.Every, defaultHeartbeatEvery)
	idle := parseSure(h.Idle, defaultHeartbeatIdle)

	return &heartbeatRunner{
		client:   client,
		opts:     opts,
		interval: interval,
		idle:     idle,
	}
}

// defaultHeartbeatEvery, kontrol sıklığının varsayılanı.
const defaultHeartbeatEvery = time.Minute

// defaultHeartbeatIdle, sessizlik eşiğinin varsayılanı.
const defaultHeartbeatIdle = 15 * time.Minute

// parseSure, "15m" gibi bir süreyi çözer; bozuk ya da hatalıysa fallback döner.
func parseSure(raw string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}

// run, kalp atışını bağlam iptal olana dek çalıştırır.
//
// Kontrol, tetikleyicinin kendi akışından **bağımsızdır**: bir sohbet sessizse
// o sohbetin kuralı çalışmasa bile uyarı üretilir. Bu yüzden goroutine
// dışında, ana akışa paralel yürür.
func (h *heartbeatRunner) run(ctx context.Context, engine *engine) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Config her turda yeniden okunur: kullanıcı `heartbeat:` bloğunu
			// düzenleyip kaydedince restart gerekmez — tıklama yeter.
			set := engine.rules()
			if !set.Heartbeat.enabled() {
				continue
			}
			h.tazeleOpts(set)
			h.tick(ctx, set.Heartbeat, engine)
		}
	}
}

// tazeleOpts, opencode yolunu taze config'den alır.
//
// Neden: main.run opts.opencode doldurmaz — engine her mesajda tazeler ama
// heartbeat startup kopyasını kullanır. Tazelenmezse nudge "opencode yolu
// tanımlı değil" diye düşer (2026-10-07 canlı ölçümü: 4 tur üst üste).
func (h *heartbeatRunner) tazeleOpts(set ruleSet) {
	if set.OpenCode != "" {
		h.opts.opencode = set.OpenCode
	}
}

// tick, bir turda tüm izlenen sohbetleri kontrol eder.
//
// Hata turu kesmez: bir sohbette sorun varsa diğerleri yine kontrol edilir.
func (h *heartbeatRunner) tick(ctx context.Context, hb heartbeat, engine *engine) {
	for _, jid := range hb.To {
		if ctx.Err() != nil {
			return
		}

		// Süreler config'den gelir; bozuksa varsayılana düşer.
		idle := parseSure(hb.Idle, defaultHeartbeatIdle)

		last, err := h.client.lastActivity(ctx, jid)
		if err != nil {
			logf("kalp atışı: %s etkinlik okunamadı: %v", jid, err)
			continue
		}

		if !last.isIdle(idle, time.Now()) {
			continue
		}

		logf("kalp atışı: %s %d dk sessiz → uyarı gönderiliyor",
			jid, int(idle.Minutes()))
		h.nudge(ctx, engine, jid, hb.Mesaj)
	}
}

// nudge, uyarıyı ajanın sabit oturumuna verir.
//
// Konuşma tetiklenmez: bu bir kural değil, doğrudan oturuma metin. Ajanın
// cevabı kendi kuralı üzerinden sohbete gider (orada zaten `not_prefix: /`
// var, o cevap normal bir mesaj gibi düşer).
func (h *heartbeatRunner) nudge(ctx context.Context, engine *engine, jid, mesaj string) {
	p := payload{
		Trigger: message{
			ID:        "heartbeat-" + jid,
			From:      jid,
			Text:      mesaj,
			Direction: "in",
		},
		History: []message{{
			ID:        "heartbeat-" + jid,
			From:      jid,
			Text:      mesaj,
			Direction: "in",
		}},
		ConfigPath: engine.configPath,
	}

	act := action{
		Kind:      "session",
		SessionID: engine.snapshot().Heartbeat.SessionID,
		Reply:     replyTarget{On: true},
	}

	runCtx, cancel := context.WithTimeout(ctx, h.opts.actionTimeout)
	defer cancel()

	reply, err := runAction(runCtx, h.opts, act, p, h.client, false, false)
	if err != nil {
		logf("  kalp atışı BAŞARISIZ: %v", err)
		return
	}
	logf("  kalp atışı cevabı gitti (%d karakter)", len(reply))
}

// activity, bir sohbetteki son hareketin zamanı.
type activity struct {
	// At, son mesajın unix zamanı. 0 = mesaj yok.
	At int64
}

// isIdle, son hareket eşikten eskiyse true döner.
//
// Mesaj hiç yoksa (At == 0) **sessiz sayılır**: sohbet yeni açıldıysa uyarı
// üretmek istemeyiz, çünkü kullanıcı henüz konuşmamış olabilir. Bu yüzden
// eşik hesabı yapılamıyorsa false döner.
func (a activity) isIdle(idle time.Duration, now time.Time) bool {
	if a.At == 0 {
		return false
	}
	return now.Sub(time.Unix(a.At, 0)) >= idle
}

// lastActivity, bir sohbetteki en yeni mesajın zamanını döner.
//
// count=1 yalnız tampondan karşılanır; köprü WhatsApp'a sormaz, yani bu
// çağrı hızlıdır ve kalp atışını bloklamaz.
func (c *bridgeClient) lastActivity(ctx context.Context, jid string) (activity, error) {
	msgs, err := c.history(ctx, jid, 1)
	if err != nil {
		// "geçmiş boş" bir hata değil: sohbet yeni açılmış demektir. Kalp atışı
		// bunu hata sayıp günlüğe doldurursa, kullanıcı henüz konuşmamışken
		// günlük anlamlı hatalarla dolar.
		if strings.Contains(err.Error(), "geçmiş boş") {
			return activity{}, nil
		}
		return activity{}, fmt.Errorf("geçmiş okunamadı: %w", err)
	}
	if len(msgs) == 0 {
		return activity{}, nil
	}

	var newest int64
	for _, m := range msgs {
		if m.Timestamp > newest {
			newest = m.Timestamp
		}
	}
	return activity{At: newest}, nil
}
