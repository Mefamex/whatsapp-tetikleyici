// whatsapp-bridge-project/tetikleyici/bekleme_test.go
//
// RATE LIMIT GERİ ÇEKİLMESİ.
//
// Buradaki testler "kullanıcıya doğru sebep söyleniyor" ve "sistem kendini
// durduruyor" diye iki şeyi sınar. İkisi de zorunlu: biri olmadan kullanıcı
// ya yanlış sebebi görür ya da hiçbir şey öğrenmez.

package main

import (
	"strings"
	"testing"
	"time"
)

// saat, testte zamanı elle ilerletebilmek için sahte saat kurar.
func saat(b *bekleme, t time.Time) { b.simdi = func() time.Time { return t } }

// ── hata tanıma ───────────────────────────────────────────────────────────

func TestRateLimitHatalariTaninir(t *testing.T) {
	olmalı := []string{
		"exit status 1: Rate limit exceeded",
		"API error: rate_limit reached",
		"HTTP 429 Too Many Requests",
		"429",
		"insufficient_quota: you exceeded your current quota",
		"quota exceeded",
		"RateLimitError",
	}
	for _, h := range olmalı {
		if bulundu, _ := rateLimitMi(h); !bulundu {
			t.Errorf("%q rate limit sayılmadı", h)
		}
	}
}

// ⛔ YANLIŞ SEBEP BİLDİRMEK, SEBEP BİLDİRMEMEKTEN KÖTÜDÜR.
//
// Kullanıcı "geçici hata, tekrar dene" sanıp üst üste dener ve asıl sorun
// (kota) hiç çözülmez. Bu yüzden "limit" tek başına ARANMAZ.
func TestLimitKelimesiTekBasinaSayilmaz(t *testing.T) {
	olmadı := []string{
		"hata: dosya yolu /home/mfmx/limit dosyası bulunamadı",
		"connection limit reached", // sahte pozitif riski: emin değiliz ama
		"unlimited",
		"signal: killed",
		"context deadline exceeded",
		"",
	}
	for _, h := range olmadı {
		if bulundu, sebep := rateLimitMi(h); bulundu {
			t.Errorf("%q YANLIŞLIKLA rate limit sayıldı: %s", h, sebep)
		}
	}
}

func TestRateLimitSebebiKullaniciDili(t *testing.T) {
	_, sebep := rateLimitMi("Rate limit exceeded")
	if !strings.Contains(sebep, "kota") && !strings.Contains(sebep, "yük") {
		t.Errorf("sebep kullanıcı dili değil: %q", sebep)
	}
}

// ── bekleme durumu ────────────────────────────────────────────────────────

func TestBeklemeBaslarVeBiter(t *testing.T) {
	b := yeniBekleme()
	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	saat(b, baslangic)

	b.baslat("a@lid", 10*time.Minute)

	if kalan := b.kalan("a@lid"); kalan != 10*time.Minute {
		t.Errorf("kalan = %v, want 10m", kalan)
	}

	saat(b, baslangic.Add(9*time.Minute))
	if b.kalan("a@lid") <= 0 {
		t.Error("9. dakikada bekleme bitmiş sayıldı")
	}

	saat(b, baslangic.Add(11*time.Minute))
	if kalan := b.kalan("a@lid"); kalan != 0 {
		t.Errorf("11. dakikada kalan = %v, want 0", kalan)
	}
}

// Bekleme BİTEN sohbet haritadan SİLİNMELİ. Tetikleyici günlerce açık
// kalıyor; silinmezse harita şişer ve her mesajda ölü kayıt aranır.
func TestBitenBeklemeSilinir(t *testing.T) {
	b := yeniBekleme()
	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	saat(b, baslangic)
	b.baslat("a@lid", time.Minute)

	saat(b, baslangic.Add(2*time.Minute))
	b.kalan("a@lid")

	b.mu.Lock()
	kalan := len(b.sohbet)
	b.mu.Unlock()

	if kalan != 0 {
		t.Errorf("haritada %d kayıt kaldı, want 0", kalan)
	}
}

// ⛔ UZATMA DÖNGÜSÜ: yeni hata mevcut beklemeyi UZATMAZ, korur.
//
// Eğer her hata süreyi baştan başlatsaydı, kullanıcı sürekli hata alır ve
// hiç çıkamazdı. En uzun süre kazanır.
func TestYeniHataBeklemeyiKisaltmaz(t *testing.T) {
	b := yeniBekleme()
	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	saat(b, baslangic)

	bitis := b.baslat("a@lid", 30*time.Minute)
	saat(b, baslangic.Add(1*time.Minute))

	yeni := b.baslat("a@lid", 5*time.Minute)
	if !yeni.Equal(bitis) {
		t.Errorf("bitiş %v → %v değişti; mevcut beklemeyi kısaltmamalı", bitis, yeni)
	}
	if kalan := b.kalan("a@lid"); kalan != 29*time.Minute {
		t.Errorf("kalan = %v, want 29m", kalan)
	}
}

// Aynı anda farklı sohbetler birbirini ETKİLEMEMELİ: birinin kotası
// bitmişken diğeri çalışabilmeli.
func TestBeklemeSohbeteOzeldir(t *testing.T) {
	b := yeniBekleme()
	saat(b, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))

	b.baslat("a@lid", 10*time.Minute)

	if b.kalan("b@lid") != 0 {
		t.Error("başka sohbet de beklemede sayıldı")
	}
	if b.kalan("a@lid") <= 0 {
		t.Error("bekleyen sohbet beklemede sayılmadı")
	}
}

// Bekleme SÜRESİ 0/negatif ise varsayılan uygulanır: 0 süre "hiç bekleme"
// olsaydı koruma anında kaybolurdu.
func TestBeklemeSuresizVarsayilanaDuser(t *testing.T) {
	b := yeniBekleme()
	saat(b, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))

	b.baslat("a@lid", 0)

	if kalan := b.kalan("a@lid"); kalan != rateLimitBekleme {
		t.Errorf("kalan = %v, want %v", kalan, rateLimitBekleme)
	}
}

// ── bildirim ──────────────────────────────────────────────────────────────

// ⛔ HER MESAJDA UYARI SPAM OLUR. Bir kez söyle, sonra sus.
func TestBildirimBirKezSoylenir(t *testing.T) {
	b := yeniBekleme()
	saat(b, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))
	b.baslat("a@lid", 10*time.Minute)

	if !b.bildir("a@lid") {
		t.Fatal("ilk bildirim yapılmadı — kullanıcı sebebi hiç öğrenmeyecek")
	}
	if b.bildir("a@lid") {
		t.Error("aynı bekleme için ikinci kez bildirildi (spam)")
	}
	if b.bildir("a@lid") {
		t.Error("üçüncü kez bildirildi (spam)")
	}
}

// Bekleme BİTTİKTEN SONRA yenisi başarsa tekrar söylenebilir: yeni olay
// eskisinin devamı değil.
func TestYenidenBeklemedeTekrarBildirilir(t *testing.T) {
	b := yeniBekleme()
	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	saat(b, baslangic)

	b.baslat("a@lid", time.Minute)
	b.bildir("a@lid")

	saat(b, baslangic.Add(2*time.Minute))
	b.kalan("a@lid") // silinir
	b.baslat("a@lid", time.Minute)

	if !b.bildir("a@lid") {
		t.Error("yeni beklemede tekrar bildirilmedi")
	}
}

// Beklemede OLMAYAN sohbet için bildirim yapılmamalı: kullanıcı neden
// uyarı alacağını anlamaz ve "sistem kendiliğinden konuşuyor" sanır.
func TestBeklemedeOlmayandaBildirimYok(t *testing.T) {
	b := yeniBekleme()
	saat(b, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))

	if b.bildir("a@lid") {
		t.Error("bekleme olmadan bildirim yapıldı")
	}
}

// ── kullanıcı metni ───────────────────────────────────────────────────────

// ⛔ SESSİZ GERİ ÇEKİLME YASAK. Metin ne olduğunu VE ne zaman deneyebileceğini
// söylemeli. "Biraz sonra" yeterli değil: kullanıcı sıralı birkaç kez dener
// ve vazgeçer.
func TestBeklemeMetniSebebiVeZamaniSoyer(t *testing.T) {
	metin := beklemeyeYanit(9 * time.Minute)

	if !strings.Contains(metin, "kota") {
		t.Errorf("metin sebebi söylemiyor: %q", metin)
	}
	if !strings.Contains(metin, "9") {
		t.Errorf("metin süreyi söylemiyor: %q", metin)
	}
}

// Dakikadan KISA sürelerde saniye verilir: "1 dakika" demek, 30 saniyelik
// beklemede kullanıcıyı yanıltır.
func TestKisaSurelerdeSaniyeVerilir(t *testing.T) {
	metin := beklemeyeYanit(45 * time.Second)

	if !strings.Contains(metin, "45") {
		t.Errorf("metin saniyeyi söylemiyor: %q", metin)
	}
	if strings.Contains(metin, "dakika") {
		t.Errorf("45 saniye 'dakika' olarak anlatıldı: %q", metin)
	}
}

func TestBeklemeMetniSifirSureBos(t *testing.T) {
	if metin := beklemeyeYanit(0); metin != "" {
		t.Errorf("sıfır sürede metin üretildi: %q", metin)
	}
	if metin := beklemeyeYanit(-time.Minute); metin != "" {
		t.Errorf("negatif sürede metin üretildi: %q", metin)
	}
}

// Kullanıcıya model jargonu sızmamalı: "429" ya da "quota" değil, sonuç.
func TestBeklemeMetniTeknikDegil(t *testing.T) {
	metin := beklemeyeYanit(10 * time.Minute)

	for _, jargon := range []string{"429", "HTTP", "quota", "exit status", "429 Too Many"} {
		if strings.Contains(metin, jargon) {
			t.Errorf("metin jargon içeriyor (%q): %q", jargon, metin)
		}
	}
}

// Beklemede kullanıcı TAMAMEN çaresiz kalmamalı: başka bir iş yapabileceğini
// bilmesi, pasif beklerken sinirini çıkarmaz.
func TestBeklemeMetniAlternatifSoyler(t *testing.T) {
	metin := beklemeyeYanit(10 * time.Minute)

	if !strings.Contains(metin, "/help") {
		t.Errorf("metin bir alternatif sunmuyor: %q", metin)
	}
}

// ── temizlik ──────────────────────────────────────────────────────────────

func TestTemizleSuresiDolmusKayitlariSiler(t *testing.T) {
	b := yeniBekleme()
	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	saat(b, baslangic)

	b.baslat("eski@lid", time.Minute)
	b.baslat("yeni@lid", time.Hour)

	saat(b, baslangic.Add(2*time.Minute))
	b.temizle()

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, bulunduMu := b.sohbet["eski@lid"]; bulunduMu {
		t.Error("süresi dolmuş kayıt silinmedi")
	}
	if _, bulunduMu := b.sohbet["yeni@lid"]; !bulunduMu {
		t.Error("süresi dolmamış kayıt silindi")
	}
}

// ── nil güvenliği ─────────────────────────────────────────────────────────

// Testler `&engine{}` ile kurulabiliyor; bekleme alanı boş olabilir.
// Korumanın YOKLUĞU bir panik sebebi olmamalı — panik, korumadan çok daha
// kötü bir sonuç (süreç ölür, hiçbir koruma da çalışmaz).
func TestNilBeklemePanikAtmaz(t *testing.T) {
	var b *bekleme

	if kalan := b.kalan("a@lid"); kalan != 0 {
		t.Errorf("nil beklemede kalan = %v", kalan)
	}
	if b.bildir("a@lid") {
		t.Error("nil beklemede bildirim yapıldı")
	}
	b.baslat("a@lid", time.Minute)
	b.temizle()
}

func TestKisaSureOkunur(t *testing.T) {
	vakalar := []struct {
		s    time.Duration
		want string
	}{
		{0, "0"},
		{-time.Minute, "0"},
		{30 * time.Second, "30 sn"},
		{10 * time.Minute, "10 dk"},
		{time.Hour, "60 dk"},
	}
	for _, v := range vakalar {
		if got := kisaSure(v.s); got != v.want {
			t.Errorf("kisaSure(%v) = %q, want %q", v.s, got, v.want)
		}
	}
}
