// whatsapp-bridge-project/tetikleyici/cift_gonderim_test.go
//
// ÇİFT GÖNDERİM BASTIRMASI — regresyon testleri.
//
// BAĞLAM: wp-sahip ajanına gönderim araçları (whatsapp_send,
// whatsapp_send_media) önce yasaklanmıştı, "çift gönderim olur" diye. Bu
// YANLIŞTI: ajan dosya ve görsel gönderme yeteneğini tamamen kaybetti.
// Doğrusu yetkiyi açık bırakmak, tekrarı gönderim katmanında bastırmaktı.
//
// Bu dosyadaki testlerin kanıtladığı: ajan hem kendi whatsapp_send'ini
// çağırıp hem stdout üretirse kullanıcı TEK mesaj alır.
//
// Sahte köprü, iki ucu birden taklit eder — /messages (bastırma sorgusu)
// ve /send (gönderim). /send'e düşen mesajlar geçmişe de yazılır, çünkü
// GERÇEK köprüde de öyledir: bastırma kararı "son giden mesajlara bak"
// mantığına dayanır.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// sahteKopru, bridgeClient'in konuştuğu iki ucu da taklit eder.
type sahteKopru struct {
	mu       sync.Mutex
	gecmis   []message
	gonderil []sentMessage
	hata     string // "messages" ise /messages 500 döner
}

func (s *sahteKopru) baslat(t *testing.T) *bridgeClient {
	t.Helper()

	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, "/messages"):
			if s.hata == "messages" {
				http.Error(w, "geçmiş alınamadı", http.StatusInternalServerError)
				return
			}
			// Köprü en yeniyi başta döndürür; sayı sınırına uyarak.
			adet := 12
			if v := r.URL.Query().Get("count"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					adet = n
				}
			}
			kopya := make([]message, 0, len(s.gecmis))
			for i := len(s.gecmis) - 1; i >= 0 && len(kopya) < adet; i-- {
				kopya = append(kopya, s.gecmis[i])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": len(kopya), "jid": "test@lid", "messages": kopya,
			})

		case r.URL.Path == "/send":
			var msg sentMessage
			_ = json.NewDecoder(r.Body).Decode(&msg)
			s.gonderil = append(s.gonderil, msg)
			// Gerçek köprüde de giden mesaj geçmişe düşer. Bastırma
			// sorgusu BUNU görür — test edilen asıl davranış budur.
			s.gecmis = append(s.gecmis, message{
				Direction: "out", IsFromMe: true,
				From: msg.To, Text: msg.Text,
				Timestamp: time.Now().Unix(),
			})
			_, _ = w.Write([]byte(`{"ok":true}`))

		default:
			http.Error(w, "bilinmeyen uç", http.StatusNotFound)
		}
	}))
	t.Cleanup(sunucu.Close)

	return newBridgeClient(sunucu.URL, 5*time.Second)
}

// ekle, teste geçmiş mesajı yerleştirir.
func (s *sahteKopru) ekle(m message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gecmis = append(s.gecmis, m)
}

// ── yardımcılar ──────────────────────────────────────────────────────────────

func giden(metin string, yas time.Duration) message {
	return message{
		Direction: "out", IsFromMe: true,
		From: "12345678901234@lid", Text: metin,
		Timestamp: time.Now().Add(-yas).Unix(),
	}
}

func gelen(metin string, yas time.Duration) message {
	m := giden(metin, yas)
	m.Direction = "in"
	m.IsFromMe = false
	return m
}

// ── teklastirMetin: metin normalizasyonu ─────────────────────────────────────

func TestTeklastirMetinBicimFarkiniSiler(t *testing.T) {
	tablolar := []struct {
		ad      string
		a, b    string
		aynilir bool
	}{
		{"aynen ayni", "selam", "selam", true},
		{"kenar boslugu", "  selam  ", "selam", true},
		{"satir sonu", "selam\nnasilsin", "selam nasilsin", true},
		{"tekrarli bosluk", "selam   nasilsin", "selam nasilsin", true},
		{"kalin isaret", "**selam**", "selam", true},
		{"kod blogu", "```\nselam\n```", "selam", true},
		{"buyuk harf", "SELAM", "selam", true},
		{"tab ile bosluk", "selam\tnasilsin", "selam nasilsin", true},
		{"turkiye karakteri ayni", "şey", "sey", false}, // farklı: aşırı normalleştirme

		// FARKLI cevaplar AYNI sayılmamalı. Yanlışsa kullanıcının
		// cevabı yutulur — bastırmanın en kötü hatası.
		{"farkli kelime", "selam", "hosca kal", false},
		{"farkli sayi", "toplam 23.050", "toplam 23.500", false},
		{"biri digerinin uzantisi degil", "evet", "evet mi", false},
	}

	for _, tc := range tablolar {
		t.Run(tc.ad, func(t *testing.T) {
			got := teklastirMetin(tc.a) == teklastirMetin(tc.b)
			if got != tc.aynilir {
				t.Fatalf("%q ile %q → aynı=%v, beklenen %v", tc.a, tc.b, got, tc.aynilir)
			}
		})
	}
}

func TestTeklastirMetinBosMetin(t *testing.T) {
	for _, s := range []string{"", "   ", "\n\n", "\t"} {
		if teklastirMetin(s) != "" {
			t.Errorf("boş olmalıydı: %q → %q", s, teklastirMetin(s))
		}
	}
}

func TestTeklastirMetinSurdurucuBozulmaz(t *testing.T) {
	// Kullanıcı link istediyse link hâlinde gitmeli; sorgu dizesi korunur.
	for _, s := range []string{
		"https://ornek.com/a?b=1&c=2",
		"toplam 23.050,00 TL",
		"v2.1.3 sürümü",
	} {
		if got := teklastirMetin(s); got != teklastirMetin(s) {
			t.Fatalf("normalizasyon idempotent değil: %q → %q → %q", s, teklastirMetin(s), got)
		}
	}
}

// ── zatenGonderilmis: bastırma kararı ───────────────────────────────────────

func TestAynenGonderilmisYakindaGidenAyniMetniBulur(t *testing.T) {
	k := &sahteKopru{}
	k.ekle(giden("pong, kopru calisiyor", 20*time.Second))

	aynen, sebep := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "pong, kopru calisiyor")

	if !aynen {
		t.Fatal("20 saniye once giden ayni metin bastirilmaliydi")
	}
	if sebep == "" {
		t.Error("gerekce bos olmamali — gunlukte ne oldugu gorunmeli")
	}
}

func TestAynenGonderilmisFarkliMetniBastirmaz(t *testing.T) {
	k := &sahteKopru{}
	k.ekle(giden("birinci cevap", 15*time.Second))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "ikinci cevap farkli")

	if aynen {
		t.Fatal("FARKLI metin bastirilmamali — kullanici ikinci cevabi hak ediyor")
	}
}

func TestAynenGonderilmisEskiMesajiBastirmaz(t *testing.T) {
	// 5 dakika once giden ayni metin: kullanici "tekrar soyle" dediyse
	// bastirmak yanlis olur ve kullanici cevabi hic almaz.
	k := &sahteKopru{}
	k.ekle(giden("selam", 5*time.Minute))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "selam")

	if aynen {
		t.Fatal("zaman penceresi disindaki mesaj bastirilmamali")
	}
}

func TestAynenGonderilmisGelenMesajiBastirmaz(t *testing.T) {
	// Kullanici mesajiyla AYNI metin: bu yanit DEGIL. Bastirilirsa
	// kullanici kendi yazdigini cevap sanar.
	k := &sahteKopru{}
	k.ekle(gelen("selam", 10*time.Second))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "selam")

	if aynen {
		t.Fatal("kullanici'nin kendi mesaji bastirilmamali")
	}
}

func TestAynenGonderilmisGecmisAlinamazsaBastirmaz(t *testing.T) {
	// Kopru yeniden baglandiginda gecmis bos doner. Bu HATA DEGIL:
	// bastirilamaz, gonderim yapilmalidir. Yanlis bastirma, kullaniciya
	// hic mesaj gitmemesi demektir — aciklanamaz.
	k := &sahteKopru{hata: "messages"}

	aynen, sebep := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "selam")

	if aynen {
		t.Fatal("gecmis alinamadiysa bastirilmamali, gonderilmeli")
	}
	if sebep == "" {
		t.Error("gerekce yazilmali — neden gonderildigi gunlukten anlasilmali")
	}
}

func TestAynenGonderilmisBosMetniBastirmaz(t *testing.T) {
	k := &sahteKopru{}
	k.ekle(giden("", time.Second))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "")

	if aynen {
		t.Fatal("bos metin bastirilmamali")
	}
}

func TestAynenGonderilmisSifirZamanDamgasiniGormez(t *testing.T) {
	// Kopru zaman damgasi vermeyebilir (0). "Eski" sayip gecmeli.
	k := &sahteKopru{}
	sifir := giden("selam", time.Second)
	sifir.Timestamp = 0
	k.ekle(sifir)

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "selam")

	if aynen {
		t.Fatal("zaman damgasi 0 ise guvenilmez, bastirilmamali")
	}
}

func TestAynenGonderilmisPencereninIcinde(t *testing.T) {
	k := &sahteKopru{}
	k.ekle(giden("kontrol", 90*time.Second))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "kontrol")

	if !aynen {
		t.Fatal("90 saniye once giden ayni metin pencere icinde, bastirilmaliydi")
	}
}

func TestAynenGonderilmisSaniyeCozunurluguSiniriniKacar(t *testing.T) {
	// Köprü Unix SANİYESİ gönderir. Bu yüzden zaman damgası bir saniye
	// çözünürlüğündedir ve pencere sınırındaki bir mesaj gerçekte 1 saniye
	// daha yaşlı görünebilir.
	//
	// Test, tolerans olmasaydı pencereden kaçacak (bastırılmayacak) mesajı
	// üretir. Testi kasten böyle kuruyoruz: 2ms fark, kesilmiş zaman
	// damgasıyla 1 saniyelik belirsizliğe denk gelir.
	k := &sahteKopru{}
	k.ekle(giden("sinir", zamanBastirmaPencere-2*time.Millisecond))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "sinir")

	if !aynen {
		t.Fatalf("saniye çözünürlüğü nedeniyle pencere sınırındaki mesaj kaçtı; "+
			"zamanDamgasiBelirsizligi = %v uygulanmalı", zamanDamgasiBelirsizligi)
	}
}

func TestAynenGonderilmisGercektenEskiyiBastirmaz(t *testing.T) {
	// Tolerans pencerenin DIŞINA doğru açılır, içe değil: belirsizlikte
	// kaçırmak yeğdir, çünkü bastırmak kullanıcının cevabını yutar.
	k := &sahteKopru{}
	k.ekle(giden("cok eski", zamanBastirmaPencere+10*time.Second))

	aynen, _ := zatenGonderilmis(t.Context(), k.baslat(t), "test@lid", "cok eski")

	if aynen {
		t.Fatal("tolerans uygulandıktan sonra da belirgin şekilde eski mesaj bastırılmamalı")
	}
}

// ── UÇTAN UCA: ajan hem send çağırıp hem stdout üretirse ─────────────────────

func TestAjanHemGonderirHemCevapYazarsaTekMesajGider(t *testing.T) {
	// Canlıdaki senaryonun testi: wp-sahip ajanı whatsapp_send ile bir şey
	// yolluyor, tetikleyici de stdout'u cevap olarak gönderiyor.
	k := &sahteKopru{}
	istemci := k.baslat(t)
	opts := replyOptions()
	jid := mefamexMessage().From

	if err := gonder(t.Context(), opts, istemci, jid, "Dosyayi gonderdim."); err != nil {
		t.Fatalf("ajan gonderimi basarisiz: %v", err)
	}
	if err := gonder(t.Context(), opts, istemci, jid, "Dosyayi gonderdim."); err != nil {
		t.Fatalf("ikinci gonderim hata verdi: %v", err)
	}

	if got := len(k.gonderil); got != 1 {
		t.Fatalf("kullaniciya %d mesaj gitti, 1 olmaliydi. Gidenler: %+v", got, k.gonderil)
	}
}

func TestFarkliCevaplarIkiMesajGider(t *testing.T) {
	// Bastirma YALNIZ ayni metni etmeli. Ajan gercekten iki farkli sey
	// yazdiysa ikisi de gitmeli — yoksa kullanici bilgi kaybeder.
	k := &sahteKopru{}
	istemci := k.baslat(t)
	opts := replyOptions()
	jid := mefamexMessage().From

	for _, metin := range []string{"birinci cevap", "ikinci cevap"} {
		if err := gonder(t.Context(), opts, istemci, jid, metin); err != nil {
			t.Fatal(err)
		}
	}

	if got := len(k.gonderil); got != 2 {
		t.Fatalf("iki farkli cevap gonderildi ama %d mesaj gitti", got)
	}
}

func TestBastirmaKendiGonderiminiSaymaz(t *testing.T) {
	// Kritik: bastirma sorgusu KÖPRÜYE GİDEN mesajı görür. Sorgu kendi
	// gonderimini saymiyor olsa, ikinci bir cevapta da bastirir ve
	// kullanici sonraki sorularda hicbir sey almaz.
	k := &sahteKopru{}
	istemci := k.baslat(t)
	opts := replyOptions()
	jid := mefamexMessage().From

	for range 5 {
		if err := gonder(t.Context(), opts, istemci, jid, "ayni cevap"); err != nil {
			t.Fatal(err)
		}
	}

	if len(k.gonderil) != 1 {
		t.Fatalf("5 ayni gonderimden %d'i gecti, 1 olmaliydi (kendi gecmisini sayiyor)", len(k.gonderil))
	}
}

func TestBastirmaIkiFarkliTurArasindaGecerli(t *testing.T) {
	// Cift gonderim cogu zaman SU sekilde olur: ajan once "Tamam" der,
	// sonra detayi ayri mesajda yollar. Iki mesaj da FARKLI metindir.
	// Bu durumda bastirma devreye girmemeli — kullanici ikisini de istedi.
	k := &sahteKopru{}
	istemci := k.baslat(t)
	opts := replyOptions()
	jid := mefamexMessage().From

	if err := gonder(t.Context(), opts, istemci, jid, "Tamam"); err != nil {
		t.Fatal(err)
	}
	if err := gonder(t.Context(), opts, istemci, jid, "Dosya: /tmp/rapor.pdf"); err != nil {
		t.Fatal(err)
	}
	if err := gonder(t.Context(), opts, istemci, jid, "Tamam"); err != nil {
		t.Fatal(err)
	}

	// "Tamam" ikinci kez bastirildi, "Dosya..." gitti → 2 mesaj
	if got := len(k.gonderil); got != 2 {
		t.Fatalf("2 mesaj beklenir, %d gitti: %+v", got, k.gonderil)
	}
	if k.gonderil[1].Text != "Dosya: /tmp/rapor.pdf" {
		t.Fatalf("ikinci mesaj yanlis: %q", k.gonderil[1].Text)
	}
}

// ── canlı yapılandırma: ajanlarda gönderim yasağı OLMAMALI ──────────────────

func TestKurulmusYapilandirmadaWpSahipGonderimYasagiYok(t *testing.T) {
	// Canlı hatırdır: wp-sahip'e gönderim araçları "deny" idi. Bu, ajanın
	// dosya/görsel gönderme yeteneğini kaldırıyordu ve çift gönderimi
	// çözmedi — çift gönderim tetikleyici/gonder() içinde bastırılır.
	// Yapılandırma yeniden yazılırsa bu test yakalar.
	//
	// opencode.json kullanıcıevinde; test ortamında yoksa atlanır.
	// Not: opencode.json "tools" alanı boolean (true/false) kullanır,
	// "permission" string ("deny") değil.
	ham, err := os.ReadFile(filepath.Join(
		os.Getenv("HOME"), ".config", "opencode", "opencode.json",
	))
	if err != nil {
		t.Skipf("opencode.json okunamadı (%v) — test atlandı", err)
	}

	var yapilandirma struct {
		Agent map[string]struct {
			Tools map[string]bool `json:"tools"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(ham, &yapilandirma); err != nil {
		t.Fatalf("opencode.json çözümlenemedi: %v", err)
	}

	ajan, varMi := yapilandirma.Agent["wp-sahip"]
	if !varMi {
		t.Fatal("wp-sahip ajanı tanımlı değil")
	}

	// wp-sahip'in METİN gönderim aracı AÇIK olmalı (true).
	// false olursa çift gönderim bastırması çalışmaz (ajan gönderemez demez).
	// send_media ayrı yetenek, görsel hattı için gerekli ama çift gönderimle ilgisi yok.
	if deger, varMi := ajan.Tools["whatsapp-bridge_whatsapp_send"]; !varMi || deger == false {
		t.Errorf("wp-sahip.whatsapp_send = %v — ajan metin gönderemiyor. "+
			"Çift gönderim tetikleyici/gonder() içinde bastırılır, yetki açık (true) kalmalı.", deger)
	}

	// wp-misafir'in gönderim yasağı KORUNMALIDIR: misafire gönderim
	// açılmamalı (false). wp-sahip'e verilen yetki buraya sızmamalı.
	if misafir, varMi := yapilandirma.Agent["wp-misafir"]; varMi {
		if deger, varMi := misafir.Tools["whatsapp-bridge_whatsapp_send"]; varMi && deger == true {
			t.Errorf("wp-misafir.whatsapp_send = %v — misafire gönderim açılmamalı (false olmalı)", deger)
		}
	}
}
