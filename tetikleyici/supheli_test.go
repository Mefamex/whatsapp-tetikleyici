// whatsapp-bridge-project/tetikleyici/supheli_test.go

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// supheliMesaj, şüpheli uyarı testleri için mesaj kurar. Varsayılan istenen
// alanları taşır; null değerler Go zero-value'dur.
func supheliMesaj(id, tip, from, metin, isim string) message {
	return message{ID: id, Type: tip, From: from, Text: metin, PushName: isim}
}

// supheliEngine, açık tehlikeli_mesaj bloğu + mefamex kuralı taşıyan bir
// engine kurar. aralik boşsa varsayılan (1h) kullanılır.
func supheliEngine(t *testing.T, acik bool, to, aralik string) *engine {
	t.Helper()
	var acikSatir string
	if acik {
		acikSatir = "acik: true\n"
	} else {
		acikSatir = "acik: false\n"
	}
	aralikSatir := ""
	if aralik != "" {
		aralikSatir = "  aralik: " + aralik + "\n"
	}

	body := "tehlikeli_mesaj:\n" +
		"  " + acikSatir +
		"  to: " + to + "\n" +
		aralikSatir +
		"rules:\n" +
		"- name: mefamex\n" +
		"  match: { number: [\"905551234567\", \"12345678901234@lid\"] }\n" +
		"  do: [ { type: command, code: \"true\" } ]\n"
	return newEngine(writeRules(t, body), testOptions())
}

// ── supheliSinyal ─────────────────────────────────────────────────────

func TestSupheliSinyalLink(t *testing.T) {
	sebep, supheli := supheliSinyal(supheliMesaj("m1", "text", "x@lid", "Bak şu bağlantıya: https://tuhaf-site.example/hesap-dogrula", ""))
	if !supheli || sebep != "link" {
		t.Errorf("https linkli mesaj şüpheli olmalı: sebep=%q supheli=%v", sebep, supheli)
	}

	sebep, supheli = supheliSinyal(supheliMesaj("m2", "text", "x@lid", "www.ornek.com sınav sonucu", ""))
	if !supheli || sebep != "link" {
		t.Errorf("www linkli mesaj şüpheli olmalı: sebep=%q supheli=%v", sebep, supheli)
	}

	sebep, supheli = supheliSinyal(supheliMesaj("m3", "text", "x@lid", "merhaba nasılsın", ""))
	if supheli {
		t.Errorf("düz metin şüpheli sayılmamalı: sebep=%q", sebep)
	}
}

func TestSupheliSinyalEk(t *testing.T) {
	for _, tip := range []string{"image", "video", "audio", "document"} {
		sebep, supheli := supheliSinyal(supheliMesaj("m", tip, "x@lid", "", ""))
		if !supheli || sebep != "ek: "+tip {
			t.Errorf("%s eki şüpheli olmalı: sebep=%q supheli=%v", tip, sebep, supheli)
		}
	}

	for _, tip := range []string{"text", "sticker", "reaction", "poll", "protocol"} {
		_, supheli := supheliSinyal(supheliMesaj("m", tip, "x@lid", "merhaba", ""))
		if supheli {
			t.Errorf("%s tipi şüpheli sayılmamalı", tip)
		}
	}
}

// ── taninanKimlik ─────────────────────────────────────────────────────

func TestSupheliTaninanKimlikNumara(t *testing.T) {
	set := ruleSet{Rules: []rule{{Match: condition{Numbers: []string{"905551234567"}}}}}
	m := supheliMesaj("m1", "text", "905551234567@s.whatsapp.net", "link http://x.com", "")
	if !taninanKimlik(set, m) {
		t.Error("kuralda yazılı numara tanınmalı")
	}
}

func TestSupheliTaninanKimlikIsim(t *testing.T) {
	set := ruleSet{Rules: []rule{{Match: condition{Names: []string{"Mefamex"}}}}}
	m := supheliMesaj("m1", "text", "12345678901234@lid", "link http://x.com", "Mefamex")
	if !taninanKimlik(set, m) {
		t.Error("kuralda yazılı isim tanınmalı")
	}
}

func TestSupheliTaninanKimlikBilinmeyen(t *testing.T) {
	set := ruleSet{Rules: []rule{{Match: condition{Numbers: []string{"905551234567"}}}}}
	m := supheliMesaj("m1", "text", "905123456789@s.whatsapp.net", "link http://x.com", "Yabancı")
	if taninanKimlik(set, m) {
		t.Error("kuralda olmayan numara tanınmamalı")
	}
}

// ── config enabled ────────────────────────────────────────────────────

func TestSupheliConfigKapaliKosullari(t *testing.T) {
	// acik yazılmamış: nil pointer → kapalı.
	var kapali tehlikeliConfig
	if kapali.enabled() {
		t.Error("boş config kapalı olmalı")
	}

	// acik true ama to boş: kime gideceği belli değil → kapalı.
	evet := true
	hedefsiz := tehlikeliConfig{Acik: &evet}
	if hedefsiz.enabled() {
		t.Error("to boşken özellik kapalı olmalı")
	}

	// acik false ama to dolu: kapalı.
	hayir := false
	kapaliHedefli := tehlikeliConfig{Acik: &hayir, To: "12345678901234@lid"}
	if kapaliHedefli.enabled() {
		t.Error("acik false iken özellik kapalı olmalı")
	}
}

func TestSupheliConfigAcik(t *testing.T) {
	evet := true
	cfg := tehlikeliConfig{Acik: &evet, To: "12345678901234@lid"}
	if !cfg.enabled() {
		t.Error("acik true + to dolu iken özellik açık olmalı")
	}
}

// ── engine entegrasyonu ───────────────────────────────────────────────

// TestSupheliBilinmeyenLinkGider: tanınmayan numara, linkli mesaj →
// dispatch YANIT vermez ama admine 1 uyarı gider.
func TestSupheliBilinmeyenLinkGider(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, true, "12345678901234@lid", "")

	mesaj := supheliMesaj("m1", "text", "905123456789@s.whatsapp.net",
		"Kampanya: https://sifre-sifirla.example", "Yabancı")
	sonuc := e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)), mesaj)

	if sonuc != 0 {
		t.Errorf("eşleşmeyen mesaj kural çalıştırmamalı, sonuç=%d", sonuc)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("1 uyarı gitmeli, %d gitti: %+v", len(fake.sent), fake.sent)
	}
	if fake.sent[0].To != "12345678901234@lid" {
		t.Errorf("uyarı admine gitmeli, hedef=%q", fake.sent[0].To)
	}
	if !strings.Contains(fake.sent[0].Text, "link") {
		t.Errorf("uyarı sebep içermeli: %q", fake.sent[0].Text)
	}
}

// TestSupheliBilinmeyenDuzMetinGitmez: tanınmayan numara ama sıradan metin
// → uyarı YOK. "Kapalı hat sessizdir": her yabancı mesaj bağırtılmaz.
func TestSupheliBilinmeyenDuzMetinGitmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, true, "12345678901234@lid", "")

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)),
		supheliMesaj("m1", "text", "905123456789@s.whatsapp.net", "merhaba", "Yabancı"))

	if len(fake.sent) != 0 {
		t.Errorf("düz metin uyarı doğurmamalı: %+v", fake.sent)
	}
}

// TestSupheliTaninanKimliktenGitmez: kurallarda tanınan (mefamex) numaradan
// linkli mesaj → uyarı YOK. Uyarı yalnız bilinmeyen kaynaklar içindir.
func TestSupheliTaninanKimliktenGitmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, true, "12345678901234@lid", "")

	e.supheliUyari(context.Background(), client, e.rules(),
		supheliMesaj("m1", "text", "905551234567@s.whatsapp.net", "http://x.com", "Mefamex"))

	if len(fake.sent) != 0 {
		t.Errorf("tanınan kimlikten uyarı gitmemeli: %+v", fake.sent)
	}
}

// TestSupheliKapaliykenGitmez: acik: false → eşleşmeyen şüpheli mesajda bile
// uyarı YOK. MEVZU: varsayılan kapalı, açık kapı yok.
func TestSupheliKapaliykenGitmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, false, "12345678901234@lid", "")

	e.dispatch(context.Background(), client, newSeenStore(yeniSeenYolu(t)),
		supheliMesaj("m1", "text", "905123456789@s.whatsapp.net", "http://x.com", "Yabancı"))

	if len(fake.sent) != 0 {
		t.Errorf("kapalıyken uyarı gitmemeli: %+v", fake.sent)
	}
}

// TestSupheliFromMeGitmez: kendi ürettiğimiz bir mesaj uyarı doğurmaz
// (köprü "out" yönlü yeniden yayınlayabilir).
func TestSupheliFromMeGitmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, true, "12345678901234@lid", "")

	kendi := supheliMesaj("m1", "text", "kanal@lid", "http://x.com", "")
	kendi.IsFromMe = true
	e.supheliUyari(context.Background(), client, e.rules(), kendi)

	if len(fake.sent) != 0 {
		t.Errorf("kendi mesajımız uyarı doğurmamalı: %+v", fake.sent)
	}
}

// TestSupheliAralikIcindeTekrarGitmez: aynı numaradan iki şüpheli mesaj
// aralık (burada 1m) içindeyse yalnız ilki uyarır — spam koruması.
func TestSupheliAralikIcindeTekrarGitmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	e := supheliEngine(t, true, "12345678901234@lid", "1m")
	seen := newSeenStore(yeniSeenYolu(t))

	ilk := supheliMesaj("m1", "text", "905123456789@s.whatsapp.net", "http://a.com", "Yabancı")
	ikinci := supheliMesaj("m2", "text", "905123456789@s.whatsapp.net", "http://b.com", "Yabancı")

	e.dispatch(context.Background(), client, seen, ilk)
	e.dispatch(context.Background(), client, seen, ikinci)

	if len(fake.sent) != 1 {
		t.Errorf("aralık içinde yalnız 1 uyarı gitmeli, %d gitti: %+v", len(fake.sent), fake.sent)
	}
}

// TestSupheliAralikDoluncaTekrarGider: süre geçince aynı numara yeniden
// uyarabilir (doğrudan izleyici davranışı — saati taşımak yerine test
// doğrudan izleyiciyi kurar).
func TestSupheliAralikDoluncaTekrarGider(t *testing.T) {
	izleyici := yeniSupheliIzleyici()
	simdi := time.Now()

	if !izleyici.izinli("n", time.Hour, simdi) {
		t.Fatal("ilk uyarı izinli olmalı")
	}
	izleyici.kaydet("n", simdi)
	if izleyici.izinli("n", time.Hour, simdi) {
		t.Fatal("aralık içinde ikinci uyarı izinli olmamalı")
	}
	if !izleyici.izinli("n", time.Hour, simdi.Add(2*time.Hour)) {
		t.Fatal("süre geçince uyarı tekrar izinli olmalı")
	}
	if !izleyici.izinli("baska", time.Hour, simdi) {
		t.Fatal("farklı numara bağımsız olmalı")
	}
}

// TestSupheliMetniKisa: uyarı metni kısa, satırlı ve kimlik + sebep içerir.
func TestSupheliMetniKisa(t *testing.T) {
	metin := supheliMetni(
		supheliMesaj("m1", "text", "905123456789@s.whatsapp.net",
			"kampanya https://cok-uzun-bir-linkle-doldurulmus-site.example/yeniden-giris", "Yabancı"),
		"link")

	if len(metin) > 160 {
		t.Errorf("uyarı metni kısa olmalı (%d karakter): %q", len(metin), metin)
	}
	for _, gerekli := range []string{"Şüpheli", "Yabancı", "link"} {
		if !strings.Contains(metin, gerekli) {
			t.Errorf("uyarı %q içermeli: %q", gerekli, metin)
		}
	}
}
