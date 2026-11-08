// whatsapp-bridge-project/tetikleyici/supheli.go
//
// Tehlikeli mesaj uyarısı: kurallarda TANINMAYAN numaralardan gelen şüpheli
// mesajlar (metinde link VEYA dosya/medya eki) admin'e bildirilir.
//
// ⛔ KALICI KURAL saygısı — "kapalı hat sessizdir" (2026-10-06):
//   · Uyarı bir YANIT değildir: gelen numaraya HİÇBİR ŞEY gönderilmez.
//   · Kurallarda tanınan kimliklerden (mefamex dâhil) gelen hiçbir mesaj
//     uyarıya girmez.
//   · Yalnız mesaj KİMSEYE eşleşmiyorsa VE numara hiç tanınmıyorsa tarama
//     yapılır. Böylece kapalı kurallı kişilerden (Burak/Yusuf/Masha) gelen
//     NORMAL mesajlar admin'e "şüpheli" diye düşmez.
//
// Sahte pozitif zararsızdır: uyarı yalnız admin'e gider; sahte negatif
// (kaçırılan bir sinyal) sessizdir — özellik kapanabilir, hat hiçbir zaman
// bir kullanıcıyı yanlış cevaplamaz.

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// varsayilanSupheliAralik, aynı numaradan uyarılar arası asgari süredir.
const varsayilanSupheliAralik = time.Hour

// supheliSinyal, mesajın şüpheli sayılıp sayılmadığını ve sebebini bildirir.
//
// Sinyaller (şimdilik): metinde URL/link VEYA dosya/medya eki. İkisi de
// kötü niyetli trafiğin en sık taşıdığı yüktür (kimlik avı linki, bulaşık
// dosya). Metinsiz mesajlar link içeremez; ek alanı onlar için yeter.
func supheliSinyal(m message) (sebep string, supheli bool) {
	metin := strings.ToLower(m.Text)
	if strings.Contains(metin, "http://") || strings.Contains(metin, "https://") ||
		strings.Contains(metin, "www.") {
		return "link", true
	}
	switch m.Type {
	case "image", "video", "audio", "document":
		return "ek: " + m.Type, true
	}
	return "", false
}

// taninanKimlik, mesajın kaynağının kurallarda tanındığını bildirir.
//
// Numaralar (From/Sender) ve isimler (PushName) ayrı ayrı denenir; LID'li
// sohbetlerde numara payload'da olmaz, isim kalır. Herhangi bir eşleşme
// yeterlidir — tanınan biri uyarıya HİÇ girmez.
func taninanKimlik(set ruleSet, m message) bool {
	for _, r := range set.Rules {
		for _, num := range r.Match.Numbers {
			num = strings.TrimSpace(num)
			if num == "" {
				continue
			}
			if containsFold(m.From, num) || containsFold(m.Sender, num) {
				return true
			}
		}
		for _, isim := range r.Match.Names {
			if containsFold(m.PushName, isim) {
				return true
			}
		}
	}
	return false
}

// supheliIzleyici, aynı numaradan uyarıların aralığını bellekte tutar.
//
// Restart'ta sıfırlanır: o an aynı numara bir kez daha uyarabilir. Zararsız
// — uyarı yanıt değildir ve numara kümesi küçüktür; kalıcı dosya, seen
// defteri gibi yazma kaygısı bu kadar hafif bir özellik için gerekmiyor.
type supheliIzleyici struct {
	mu  sync.Mutex
	son map[string]time.Time
}

// yeniSupheliIzleyici, boş bir izleyici kurar.
func yeniSupheliIzleyici() *supheliIzleyici {
	return &supheliIzleyici{son: map[string]time.Time{}}
}

// izinli, numara için aralık eşiği dolmuş mu. Damga yoksa her zaman izinli.
func (s *supheliIzleyici) izinli(numara string, aralik time.Duration, simdi time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	son, varMi := s.son[numara]
	if !varMi {
		return true
	}
	return simdi.Sub(son) >= aralik
}

// kaydet, numarayı şimdi damgalar.
func (s *supheliIzleyici) kaydet(numara string, simdi time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.son[numara] = simdi
}

// supheliUyari, eşleşmeyen ve tanınmayan bir mesaj şüpheli sinyalle
// gelirse admin'e bildirir. Yapılandırılmamışsa, tanınıyorsa ya da şüpheli
// değilse SESSİZCE geçer — üçü de "normal trafik" sayılır.
func (e *engine) supheliUyari(ctx context.Context, client *bridgeClient, set ruleSet, m message) {
	cfg := set.Tehlikeli
	if !cfg.enabled() || client == nil {
		return
	}
	if m.fromMe() || m.From == "" {
		return
	}
	if taninanKimlik(set, m) {
		return
	}
	sebep, supheli := supheliSinyal(m)
	if !supheli {
		return
	}

	aralik := parseSure(cfg.Aralik, varsayilanSupheliAralik)
	if !e.supheliBellek.izinli(m.From, aralik, time.Now()) {
		logf("şüpheli uyarı atlandı (%s): aralık içinde tekrar", kisaJID(m.From))
		return
	}

	metin := supheliMetni(m, sebep)
	sonCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.sendText(sonCtx, cfg.To, metin); err != nil {
		logf("şüpheli uyarısı gönderilemedi (%s): %v", cfg.To, err)
		return
	}

	e.supheliBellek.kaydet(m.From, time.Now())
	logf("⚠️ şüpheli uyarı gitti (%s → %s): %s", kisaJID(m.From), cfg.To, sebep)
}

// supheliMetni, admin'e gidecek kısa, satırlı uyarı metnini kurar.
//
// Kimlik satırı etiketsiz numara döner (LID olabilir); push_name varsa
// insan okunur ad öne geçer. İçerik tek satıra kırpılır — uzun bir link
// bile tek satırda kalmalı.
func supheliMetni(m message, sebep string) string {
	kimlik := m.label()
	icerik := truncate(m.readableText(), 100)

	var b strings.Builder
	b.WriteString("⚠️ Şüpheli mesaj\n")
	fmt.Fprintf(&b, "Kimden: %s\n", kimlik)
	fmt.Fprintf(&b, "Sebep : %s\n", sebep)
	if icerik != "" {
		fmt.Fprintf(&b, "İçerik: %s\n", icerik)
	}
	return b.String()
}
