// whatsapp-bridge-project/tetikleyici/bekleme_akis_test.go
//
// RATE LIMIT GERİ ÇEKİLMESİNİN UÇTAN UCA ETKİSİ.
//
// bekleme_test.go birim düzeyinde davranışı sınıyordu. Burada asıl soru
// ölçülüyor: bekleme sırasında AJAN GERÇEKTEN ÇALIŞMIYOR MU?
//
// Bu ayrım önemli. Bekleme.tablosu doğru çalışabilir ama dispatch onu
// okumuyorsa koruma yalnız tabloda kalır ve hiçbir işe yaramaz — "ölçtüğün
// şeyi ölçme" hatasının tam olarak kendisi.
//
// ⛔ SAYAÇ DOSYASI: betik ve sayaç AYRI dosyadır. Betik kendi kendine
// yazsaydı, sayım betiğin KENDİ KAYNAK METNİNİ de sayardı ve "0 kez
// çalıştı" hiçbir zaman görünmezdi. Tespit ettiğim ilk hata buydu: sayaç
// 5 gösteriyordu, oysa hiçbir şey çalışmamıştı — 5, betiğin beş kelimesiydi.

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// beklemeAractizi, sayaç üreten betiği ve sayaç yolunu döner.
//
// Betik `true` üretmek yerine sayaç dosyasına satır EKLER: kaç kez
// çalıştığı sayıdan bakan tek yöntem bu. Dönüş değeri "kaçıncı sefer"dir,
// "kaç kez" değil.
func beklemeAractizi(t *testing.T) (betik, sayac string) {
	t.Helper()

	dizin := t.TempDir()
	sayac = filepath.Join(dizin, "sayac.txt")
	betik = filepath.Join(dizin, "saya.sh")

	// Tek tırnaklı yol: yol boşluk içerse bile bozulmaz.
	icerik := "#!/bin/sh\nprintf 'x\\n' >> '" + sayac + "'\n"
	if err := os.WriteFile(betik, []byte(icerik), 0o700); err != nil {
		t.Fatal(err)
	}
	return betik, sayac
}

// sayacOku, sayaç dosyasındaki satır sayısını döner. Dosya yoksa 0.
func sayacOku(t *testing.T, sayac string) int {
	t.Helper()

	ham, err := os.ReadFile(sayac)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimRight(string(ham), "\n"), "\n"))
}

// yeniSeenYolu, her test için ayrı bir "işlendi" dosyası üretir.
//
// Neden ayrı: seenStore kalıcıdır (yeniden başlatmalarda hatırlanır). Aynı
// yolu paylaşan iki test birbirinin mesajını "zaten işlendi" sanar ve test
// kendi kendini sabote eder.
func yeniSeenYolu(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "gorulen.txt")
}

// beklemeKur, HER SOHBETİ tutan gerçek bir config + motor kurar.
//
// `match: {}` (koşulsuz) kullanılıyor: sohbet bazlı davranışı ölçmek için
// kuralın iki farklı sohbeti de tutması gerekiyor. Kimlik koşulu olsaydı
// "başka sohbet engellenmiyor" testi ölçtüğünü sandığı şeyi ölçmezdi.
func beklemeKur(t *testing.T, betik string) (*engine, *bridgeClient, *fakeBridge) {
	t.Helper()

	config := writeRules(t, `rules:
- name: herkes
  match: {}
  do:
  - type: command
    code: "`+betik+`"
    reply: true
    timeout: "5s"`)

	fake := &fakeBridge{}
	client := fake.start(t)

	opts := testOptions()
	opts.actionTimeout = 5 * time.Second

	return newEngine(config, opts), client, fake
}

const beklemeSohbet = "905550000001@s.whatsapp.net"

func beklemeMesaj(id, metin string) message {
	return message{
		ID:        id,
		Type:      "text",
		Direction: "in",
		From:      beklemeSohbet,
		PushName:  "Mefamex",
		Text:      metin,
	}
}

// ⛔ ANAHTAR TEST: bekleme sırasında eylem ÇALIŞMAMALI.
//
// Sayaç dosyasına bakarak ölçüyoruz. Dönüş değerine bakmak yanıltıcı olurdu:
// beklemede `return len(hits)` ile döndüğümüz için "işlendi" gibi görünür —
// oysa hiçbir şey çalışmadı. Sayaç dosyası gerçeği gösterir.
func TestBeklemedeEylemCalismaz(t *testing.T) {
	betik, sayac := beklemeAractizi(t)
	e, client, _ := beklemeKur(t, betik)

	// Tetikleyici: model kota dolduğunu bildirdi.
	e.bekleme.baslat(beklemeSohbet, 10*time.Minute)

	sonuc := e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m1", "merhaba"))

	if sayacOku(t, sayac) != 0 {
		t.Errorf("bekleme sırasında eylem %d kez çalıştı — koruma işe yaramadı",
			sayacOku(t, sayac))
	}
	// "İşlenmedi" dönmek yanlış olurdu: yeniden denemede aynı kontrol tekrar
	// tetiklenir ve mesaj bir daha hiç ele alınmaz.
	if sonuc == 0 {
		t.Error("beklemede mesaj 'işlenmedi' sayıldı; yeniden denemede aynı kontrol tekrar tetiklenir")
	}
}

// ⛔ SESSİZLİK YASAK: kullanıcı sebebi ÖĞRENMELİ.
//
// Sistem mesajı yutarsa kullanıcı "bozuk" sanır. Bekleme metni gitmeli.
func TestBeklemedeKullaniciyaSebepSoylenir(t *testing.T) {
	betik, _ := beklemeAractizi(t)
	e, client, fake := beklemeKur(t, betik)
	e.bekleme.baslat(beklemeSohbet, 10*time.Minute)

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m1", "merhaba"))

	if len(fake.sent) != 1 {
		t.Fatalf("bekleme uyarısı gönderilmedi (%d mesaj) — kullanıcı sebebi öğrenemez", len(fake.sent))
	}
	metin := fake.sent[0].Text
	if !strings.Contains(metin, "kota") {
		t.Errorf("uyarı sebebi söylemiyor: %q", metin)
	}
	if !strings.Contains(metin, "⏸") {
		t.Errorf("uyarı beklemeyi belirtmiyor: %q", metin)
	}
}

// Her mesajda uyarı GİTMEMELİ: bekleme 10 dakika sürüyor, 50 mesaj gelirse
// kullanıcı 50 uyarı alır. Ama tamamen susmak da yanlış (ilk mesajda söyle).
func TestBeklemedeUyariBirKezGider(t *testing.T) {
	betik, _ := beklemeAractizi(t)
	e, client, fake := beklemeKur(t, betik)
	e.bekleme.baslat(beklemeSohbet, 10*time.Minute)

	seen := newSeenStore(yeniSeenYolu(t))
	for i := 0; i < 5; i++ {
		e.dispatch(context.Background(), client, seen, beklemeMesaj("m"+string(rune('1'+i)), "merhaba"))
	}

	if len(fake.sent) != 1 {
		t.Errorf("%d uyarı gitti, want 1 (spam)", len(fake.sent))
	}
}

// ⛔ RATE LIMIT SONRASI KORUMA KURULMALI.
//
// Bu test olmadan koruma yalnız "bekleme başlatıldı" diye bir fonksiyon;
// hiçbir yerde çağrılmasa da testler yeşil olurdu.
func TestRateLimitHatasiBeklemeKurar(t *testing.T) {
	dizin := t.TempDir()
	sayac := filepath.Join(dizin, "sayac.txt")
	betik := filepath.Join(dizin, "kotali.sh")

	// Model hatası: exit 1 ve "Rate limit" metni.
	icerik := "#!/bin/sh\nprintf 'x\\n' >> '" + sayac +
		"'\necho 'Rate limit exceeded' >&2\nexit 1\n"
	if err := os.WriteFile(betik, []byte(icerik), 0o700); err != nil {
		t.Fatal(err)
	}

	e, client, _ := beklemeKur(t, betik)

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m1", "merhaba"))

	if sayacOku(t, sayac) != 1 {
		t.Fatalf("komut %d kez çalıştı, want 1 (test kurulumu bozuk olabilir)", sayacOku(t, sayac))
	}
	if e.bekleme.kalan(beklemeSohbet) <= 0 {
		t.Fatal("rate limit hatası bekleme kurmadı — sonraki her mesaj yeni kotayı yakacak")
	}
}

// Bekleme BİTTİKTEN SONRA sistem kendini çözmelidir: kullanıcı "tekrar
// denedim, yine çalışmadı" derse bu bir çözüm değil, duvar olur.
func TestBeklemeBitinceEylemYenidenCalisir(t *testing.T) {
	betik, sayac := beklemeAractizi(t)
	e, client, _ := beklemeKur(t, betik)

	baslangic := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	e.bekleme.simdi = func() time.Time { return baslangic }
	e.bekleme.baslat(beklemeSohbet, 10*time.Minute)

	// Bekleme sürerken: çalışmamalı.
	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m1", "merhaba"))
	if sayacOku(t, sayac) != 0 {
		t.Fatalf("beklemede çalıştı (%d)", sayacOku(t, sayac))
	}

	// Bekleme bitti.
	e.bekleme.simdi = func() time.Time { return baslangic.Add(11 * time.Minute) }
	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m2", "tekrar dene"))

	if sayacOku(t, sayac) != 1 {
		t.Errorf("bekleme bitince eylem %d kez çalıştı, want 1", sayacOku(t, sayac))
	}
}

// Bekleme TEK SOHBETE ait olmalı: bir sohbetin kotası bitince başka sohbet
// çalışmaya devam etmeli. Kapalı hatlara (Burak/Yusuf) sessiz kalma kuralı
// bozulmamalı.
func TestBeklemeBaskaSohbetiEngellemez(t *testing.T) {
	betik, sayac := beklemeAractizi(t)
	e, client, _ := beklemeKur(t, betik)
	e.bekleme.baslat(beklemeSohbet, 10*time.Minute)

	baska := beklemeMesaj("m9", "merhaba")
	baska.From = "905550000005@s.whatsapp.net"

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), baska)

	if sayacOku(t, sayac) != 1 {
		t.Errorf("başka sohbet engellendi (sayaç %d, want 1)", sayacOku(t, sayac))
	}
}

// Bekleme kurulmadan ÖNCE her şey eskisi gibi çalışmalı: bu koruma yalnız
// rate limit görüldükten sonra devreye giriyor. Yanlışlıkla hep açık
// kalırsa kullanıcı hiçbir isteği işletemez.
func TestBeklemeKurulmadanOnceHerSeyCalisir(t *testing.T) {
	betik, sayac := beklemeAractizi(t)
	e, client, _ := beklemeKur(t, betik)

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), beklemeMesaj("m1", "merhaba"))

	if sayacOku(t, sayac) != 1 {
		t.Errorf("bekleme yokken eylem %d kez çalıştı, want 1", sayacOku(t, sayac))
	}
}
