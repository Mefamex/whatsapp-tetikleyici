// whatsapp-bridge-project/tetikleyici/hata_ciktisi_test.go
//
// STDERR → HATA TAŞIMA.
//
// Bu dosya bir ÖLÜ kod bloğunu canlandırıyor. `cmd.Stderr = os.Stderr`
// yüzünden stderr hatada hiç görünmüyordu; bu yüzden:
//
//	· bosCiktiSebebi'nin "Rate limit" dalı  → kullanıcıya bildirim hiç gitmiyordu
//	· rateLimitMi()                          → hiç eşleşmiyordu
//	· bekleme.go geri çekilmesi              → hiç kurulmuyordu
//
// Üçü de "çalışıyor" görünüyordu ve üçü de ölüydü.

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// ── sonek davranışı ───────────────────────────────────────────────────────

func TestHataCiktisiBosKuyrukBosSonekYok(t *testing.T) {
	h := &hataCiktisi{}

	if got := h.sonek(); got != "" {
		t.Errorf("hiç stderr yazılmamışken sonek üretildi: %q", got)
	}
}

// ⛔ YAZILAN HER ŞEY HATAYA GİRMELİ — tanıma buna bağlı.
func TestHataCiktisiYazilaniHatayaTasiyor(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("API error: Rate limit exceeded"))

	if got := h.sonek(); got != ": API error: Rate limit exceeded" {
		t.Errorf("sonek = %q", got)
	}
}

// Çok satırlı stderr TEK SATIR olmalı: kullanıcıya gönderilen metinde satır
// sonları sohbeti bozar, tanımada gürültü yaratır.
func TestHataCiktisiCokSatirliTekSatirOlar(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("satır bir\nsatır iki\n\nsatır üç"))

	got := h.sonek()
	if got != ": satır bir satır iki satır üç" {
		t.Errorf("sonek = %q", got)
	}
	for _, yasak := range []string{"\n", "\r", "  "} {
		if strings.Contains(got, yasak) {
			t.Errorf("sonek hâlâ� %q içeriyor: %q", yasak, got)
		}
	}
}

// ⛔ Write TAM yazılan bayt sayısını dönmeli.
//
// MultiWriter kısa yazma görürse hata üretir. Yanlış dönüş değeri, stderr'i
// ekrana akıtırken sessizce veri kaybına yol açar.
func TestHataCiktisiTamYaziyor(t *testing.T) {
	h := &hataCiktisi{}
	p := []byte("bir iki üç")

	n, err := h.Write(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p) {
		t.Errorf("n = %d, want %d (kısa yazma)", n, len(p))
	}
}

// Uzun süreli komut sınırsız bellek tüketmemeli.
func TestHataCiktisiUzunCiktiKirpilir(t *testing.T) {
	h := &hataCiktisi{}

	parca := []byte(strings.Repeat("a", 512))
	for i := 0; i < 20; i++ {
		n, err := h.Write(parca)
		if err != nil || n != len(parca) {
			t.Fatalf("Write %d: n=%d err=%v", i, n, err)
		}
	}

	if len(h.son) > hataCiktisiMaksBayt {
		t.Errorf("kuyruk %d bayt, sınır %d aşıldı — bellek sınırsız büyüyebilir",
			len(h.son), hataCiktisiMaksBayt)
	}
}

// ⛔ Kirpma BAŞTAN atar, son N baytı KORUR.
//
// Hata mesajı genelde stderr'in sonunda biter (yığın izi, sağlayıcı mesajı).
// Başı koruyup sondan atarsak en değerli bilgi kaybolur ve tanıma bozulur —
// koruma yerine zarar verir.
//
// Beklenen değer tam olarak tanımlıdır: yazılan HER şeyin son N baytı.
// "İçinde geçiyor mu" gibi gevşek bir kontrol burada yeterli değildi; ilk
// yazımda kuyruk doğruydu ama test yine de yanlış gerekçeyle yeşildi.
func TestHataCiktisiKirpmadaTamSonBaytlariTutar(t *testing.T) {
	h := &hataCiktisi{}

	// Sınırı belirgin aşan gürültü.
	gurultu := strings.Repeat("g", hataCiktisiMaksBayt+500)
	h.Write([]byte(gurultu))
	// Asıl hata en sonda.
	asli := "Rate limit exceeded"
	h.Write([]byte(asli))

	beklenen := (gurultu + asli)[len(gurultu+asli)-hataCiktisiMaksBayt:]
	if string(h.son) != beklenen {
		t.Errorf("kuyruk son %d baytı tutmadı\n got: %q\nwant: %q",
			hataCiktisiMaksBayt, sonKısım(string(h.son), 40), sonKısım(beklenen, 40))
	}
	if !strings.Contains(string(h.son), asli) {
		t.Errorf("asıl hata mesajı korunmadı")
	}
}

// Tam sınırda yazıldığında kesilecek bayt olmamalı: sınır TAM değilse
// gereksiz veri kaybı olur.
func TestHataCiktisiKirpmaSinirdaKaybetmez(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte(strings.Repeat("b", hataCiktisiMaksBayt)))

	if len(h.son) != hataCiktisiMaksBayt {
		t.Errorf("sınırda yazıldı ama %d bayt tutuldu", len(h.son))
	}
	if got := len(h.sonek()); got > hataCiktisiMaksBayt+3 {
		t.Errorf("sonek beklenenden uzun: %d bayt", got)
	}
}

// ── cikisHatasi sarması ───────────────────────────────────────────────────

func TestCikisHatasiHataYoksaKurcalamaz(t *testing.T) {
	if err := cikisHatasi(nil, &hataCiktisi{}); err != nil {
		t.Errorf("nil hata nil dönmeliydi: %v", err)
	}

	ana := errors.New("exit status 1")
	if got := cikisHatasi(ana, &hataCiktisi{}); got != ana {
		t.Errorf("stderr yokken hata SARILMAMALI: %v", got)
	}
}

// ⛔ SARMA errors.Is/As BOZMAZ. Zaman aşımı davranışı buna dayanıyor.
func TestCikisHatasiSarmaZamanAsiminiBozmuyor(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("Rate limit exceeded"))

	sarilmis := cikisHatasi(errors.New("exit status 1"), h)

	if !errors.Is(sarilmis, context.DeadlineExceeded) {
		// Bu hata DeadlineExceeded DEĞİL, ama sarma onu da taşıyabilmeli:
		// sarmanın hedefi, sarılan hatayı olduğu gibi korumak.
		t.Log("not: sarılan hata DeadlineExceeded değil")
	}

	// Asıl kontrol: sarılan hata errors.Is ile BULUNMALI.
	ana := errors.New("çıkış hatası: exit status 1")
	if !errors.Is(cikisHatasi(ana, h), ana) {
		t.Error("sarma sarılan hatayı kaybetti — errors.Is çalışmıyor")
	}
}

func TestCikisHatasiMetniStderrEkler(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("API error: Rate limit exceeded"))

	got := cikisHatasi(errors.New("exit status 1"), h).Error()

	if !strings.Contains(got, "exit status 1") {
		t.Errorf("asıl hata kayboldu: %q", got)
	}
	if !strings.Contains(got, "Rate limit exceeded") {
		t.Errorf("stderr hataya girmedi: %q", got)
	}
}

// ── canlı yol: bosCiktiSebebi'nin ÖLÜ dala düşmesi ────────────────────────

// ⛔ BU TEST ÖNCE BAŞARISIZ OLURDU.
//
// bosCiktiSebebi içinde "Rate limit" dalı var ve çalıştığı varsayılıyordu.
// stderr hataya girmiyordu, dal hiç tetiklenmiyordu. Bu test dalın GERÇEKTEN
// erişilebilir olduğunu kanıtlar.
func TestBosCiktiSebebiRateLimitArtikErisilebilir(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("API error: Rate limit exceeded for model"))

	// Gerçek sarmalama yolundan geç.
	hata := cikisHatasi(errors.New("exit status 1"), h)

	sebep := bosCiktiSebebi(context.Background(), hata)

	if !strings.Contains(sebep, "yük altında") {
		t.Errorf("kullanıcıya rate limit sebebi gitmedi: %q", sebep)
	}
	// Teknik jargon sızmamalı.
	if strings.Contains(sebep, "exit status") {
		t.Errorf("kullanıcı metnine jargon sızdı: %q", sebep)
	}
}

// Zaman aşımı dalı stderr eklenmiş olsa bile KAZANMALI: yanlış sebep
// bildirmek, doğru sebebi gizlemekten kötüdür.
func TestCikisHatasiZamanAsimiSebebiDegistirmez(t *testing.T) {
	h := &hataCiktisi{}
	h.Write([]byte("Rate limit exceeded"))

	sarilmis := cikisHatasi(
		fmt.Errorf("çıkış hatası: %w", context.DeadlineExceeded), h)

	sebep := bosCiktiSebebi(context.Background(), sarilmis)
	if !strings.Contains(sebep, "zaman aşım") {
		t.Errorf("zaman aşımı sebebi değişti: %q", sebep)
	}
}

// ── yardımcı ──────────────────────────────────────────────────────────────

// sonKısım, metnin son n karakterini döner (hata ayıklama çıktısı için).
func sonKısım(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
