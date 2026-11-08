// whatsapp-bridge-project/tetikleyici/notify_test.go

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// bildirimTest, notify testleri için kurulu notifier döndürür.
// Damga dosyası her testte ayrı geçici dizindedir — testler birbirini
// throttle ile kirletmesin.
func bildirimTest(t *testing.T, fake *fakeBridge, cfg notifyConfig) *notifier {
	t.Helper()
	return yeniNotifier(
		fake.start(t),
		testOptions(),
		func() notifyConfig { return cfg },
		filepath.Join(t.TempDir(), "bildirim.txt"),
	)
}

// TestBildirimYapilandirilmamissaGitmez: notify bloğu yoksa hiçbir şey
// gönderilmez. "Kimse yoksa hata da yok" — kapalı hat sessizdir.
func TestBildirimYapilandirilmamissaGitmez(t *testing.T) {
	fake := &fakeBridge{}
	n := bildirimTest(t, fake, notifyConfig{})

	n.baslat(t.Context())
	n.durdur(t.Context())
	if len(fake.sent) != 0 {
		t.Errorf("yapılandırılmamış notifier mesaj gönderdi: %+v", fake.sent)
	}
}

// TestBildirimAciksaOnStartGider: on_start açık, To dolu → tek mesaj,
// mefamex LID'ine, kısa ve anlamlı metinle.
func TestBildirimAciksaOnStartGider(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{To: stringList{"12345678901234@lid"}, OnStart: true}
	n := bildirimTest(t, fake, cfg)

	n.baslat(t.Context())

	if len(fake.sent) != 1 {
		t.Fatalf("1 bildirim gitmeli, %d gitti: %+v", len(fake.sent), fake.sent)
	}
	if fake.sent[0].To != "12345678901234@lid" {
		t.Errorf("alıcı = %q, want LID", fake.sent[0].To)
	}
	if !strings.Contains(fake.sent[0].Text, "Tetikleyici açıldı") {
		t.Errorf("metin anlamlı olmalı: %q", fake.sent[0].Text)
	}
	if len(fake.sent[0].Text) > 120 {
		t.Errorf("bildirim kısa olmalı: %q", fake.sent[0].Text)
	}
}

// TestBildirimThrottleAyniTur: aynı tür, throttle penceresi içinde bir daha
// gitmez. Restart testlerinde on_start'ın tekrar gitmemesinin garantisi.
func TestBildirimThrottleAyniTur(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{To: stringList{"905550000001"}, OnStart: true, Throttle: "5m"}
	n := bildirimTest(t, fake, cfg)

	n.baslat(t.Context()) // ilk: gider
	n.baslat(t.Context()) // 5 dakika dolmadı: gitmez

	if len(fake.sent) != 1 {
		t.Errorf("throttle 5m içinde ikinci gönderim olmamalı: %d sent", len(fake.sent))
	}
}

// TestBildirimFarkliTurAyriThrottle: farklı türler ayrı damgalanır — on_start
// gittikten sonra on_stop da gidebilir.
func TestBildirimFarkliTurAyriThrottle(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{
		To: stringList{"905550000001"}, OnStart: true, OnStop: true, Throttle: "5m",
	}
	n := bildirimTest(t, fake, cfg)

	n.baslat(t.Context())
	n.durdur(t.Context())

	if len(fake.sent) != 2 {
		t.Errorf("iki farklı tür iki mesaj üretmeli: %d sent: %+v", len(fake.sent), fake.sent)
	}
	if !strings.Contains(fake.sent[0].Text, "açıldı") || !strings.Contains(fake.sent[1].Text, "durduruldu") {
		t.Errorf("sıra start→stop olmalı: %+v", fake.sent)
	}
}

// TestDamgaDosyadaKalir: damga DOSYADA tutulur — aynı dosyadan yeni bir
// defter kuran (restart simülasyonu) throttle'ı bilir. Yoksa her restart'ta
// on_start bir daha giderdi.
func TestDamgaDosyadaKalir(t *testing.T) {
	fake := &fakeBridge{}
	dizin := t.TempDir()
	yol := filepath.Join(dizin, "bildirim.txt")

	cfg := notifyConfig{To: stringList{"905550000001"}, OnStart: true}
	n := yeniNotifier(fake.start(t), testOptions(), func() notifyConfig { return cfg }, yol)
	n.baslat(t.Context())
	if len(fake.sent) != 1 {
		t.Fatalf("ilk gönderim gitti: %d", len(fake.sent))
	}

	// Yeniden başlatma: aynı dosyadan YENİ notifier.
	yeni := yeniNotifier(fake.start(t), testOptions(), func() notifyConfig { return cfg }, yol)
	yeni.baslat(t.Context())
	if len(fake.sent) != 1 {
		t.Errorf("restart sonrası throttle hatırlanmalı — ikinci gönderim olmamalı: %d sent", len(fake.sent))
	}
}

// TestBildirimHataYenidenDener: gönderim geçici başarısızsa retry kurallarıyla
// yeniden denenir; başarıda damgalanır.
func TestBildirimHataYenidenDener(t *testing.T) {
	fake := &fakeBridge{sendFailKalan: 2}
	cfg := notifyConfig{
		To: stringList{"905550000001"}, OnStart: true,
		Throttle: "5m", Retry: "10ms", MaxRetry: 5,
	}
	n := bildirimTest(t, fake, cfg)

	basla := time.Now()
	n.baslat(t.Context())

	if len(fake.sent) != 1 {
		t.Fatalf("2 geçici hata sonrası 3. denemede gitmeli: %d sent", len(fake.sent))
	}
	if time.Since(basla) > 2*time.Second {
		t.Errorf("retry 10ms ile saniyeler sürmemeli: %v", time.Since(basla))
	}

	// Başarılı gönderim DAMGALANDI: yeni defter aynı tür için 5m engelli.
	defter := yeniDamgaDefteri(n.damga.yol)
	if defter.izinli(bildirimStart, 5*time.Minute, time.Now()) {
		t.Error("başarılı gönderim damgalanmalı — yeni defter 5m içinde izin vermemeli")
	}
}

// TestBildirimVazgecer: köprü yanıt vermiyorsa maxRetry denemeden sonra
// vazgeçilir, damga YOKTUR (başarısız gönderim damgalanmaz — sonraki olay
// yeniden deneyebilsin) ve sahibe hata mesajı gitmez.
func TestBildirimVazgecer(t *testing.T) {
	fake := &fakeBridge{sendFailKalan: 99}
	cfg := notifyConfig{
		To: stringList{"905550000001"}, OnStart: true,
		Retry: "5ms", MaxRetry: 3,
	}
	n := bildirimTest(t, fake, cfg)

	n.baslat(t.Context())

	if len(fake.sent) != 0 {
		t.Errorf("hiçbir gönderim başarılı olmadı: %+v", fake.sent)
	}
	defter := yeniDamgaDefteri(n.damga.yol)
	if !defter.izinli(bildirimStart, 5*time.Minute, time.Now()) {
		t.Error("başarısız gönderim damgalanmamalı — bir sonraki olay yeniden deneyebilmeli")
	}
}

// TestBildirimIptalBaglam: bağlam iptalinde gönderim denenmez, vazgeçilir.
func TestBildirimIptalBaglam(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{To: stringList{"905550000001"}, OnStart: true}
	n := bildirimTest(t, fake, cfg)

	ctx, iptal := context.WithCancel(t.Context())
	iptal()

	n.baslat(ctx)
	if len(fake.sent) != 0 {
		t.Errorf("iptal bağlamında gönderim olmamalı: %+v", fake.sent)
	}
}

// TestKopruYalnizGecisteBildirir: köprü durumu yalnız DEĞİŞİMDE bildirilir.
// İlk gözlem sessizdir (başlangıçta düşükse "düştü" diye bağırmayız);
// tekrarlayan aynı durum yeniden bildirilmez.
func TestKopruYalnizGecisteBildirir(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{
		To: stringList{"905550000001"}, OnBridgeUp: true, OnBridgeDown: true,
	}
	n := bildirimTest(t, fake, cfg)

	kopruHatasi := errors.New("connection refused")

	n.kopru(t.Context(), nil) // ilk gözlem: SESSİZ (up)
	if len(fake.sent) != 0 {
		t.Fatalf("ilk gözlem bildirilmemeli: %+v", fake.sent)
	}

	n.kopru(t.Context(), kopruHatasi) // up → down: bildir
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0].Text, "Köprüye ulaşılamıyor") {
		t.Fatalf("düşüş bildirilmedi: %+v", fake.sent)
	}

	n.kopru(t.Context(), kopruHatasi) // down (aynı): sessiz
	if len(fake.sent) != 1 {
		t.Errorf("durum değişmedi, tekrar bildirilmemeli: %+v", fake.sent)
	}

	n.kopru(t.Context(), nil) // down → up: bildir
	if len(fake.sent) != 2 || !strings.Contains(fake.sent[1].Text, "Köprü bağlandı") {
		t.Fatalf("kalkış bildirilmedi: %+v", fake.sent)
	}

	n.kopru(t.Context(), nil) // up (aynı): sessiz
	if len(fake.sent) != 2 {
		t.Errorf("durum değişmedi, tekrar bildirilmemeli: %+v", fake.sent)
	}
}

// TestKopruKapaliBayraktaSessiz: on_bridge_down kapalıysa düşüş bildirilmez.
func TestKopruKapaliBayraktaSessiz(t *testing.T) {
	fake := &fakeBridge{}
	cfg := notifyConfig{To: stringList{"905550000001"}, OnBridgeUp: true}
	n := bildirimTest(t, fake, cfg)

	n.kopru(t.Context(), nil)                 // ilk: sessiz
	n.kopru(t.Context(), errors.New("düştü")) // up→down ama bayrak kapalı
	if len(fake.sent) != 0 {
		t.Errorf("on_bridge_down kapalıyken bildirilmemeli: %+v", fake.sent)
	}
}

// TestConfigIzleyiciDegisinceBildirir: config dosyası değişince watcher
// bildirir; değişmeden sessizdir. İlk gözlem (prime) bildirmez.
func TestConfigIzleyiciDegisinceBildirir(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "ornek.yaml")
	if err := os.WriteFile(yol, []byte("sürüm: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var bildirimler atomic.Int32
	ctx, iptal := context.WithCancel(t.Context())
	defer iptal()

	go configDegisikliginiIzle(ctx, yol, 10*time.Millisecond,
		func() bool { return true },
		func() { bildirimler.Add(1) },
	)

	time.Sleep(50 * time.Millisecond) // ilk gözlem: değişiklik yok, 0
	if got := bildirimler.Load(); got != 0 {
		t.Fatalf("değişiklik yokken bildirildi: %d", got)
	}

	if err := os.WriteFile(yol, []byte("sürüm: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Watcher'ın bir tur atmasını bekle.
	for i := 0; i < 100 && bildirimler.Load() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := bildirimler.Load(); got != 1 {
		t.Fatalf("config değişimi bildirilmedi: %d", got)
	}
}

// TestGuncelNotifyDosyadanOkur: notifier olayları mesaj akışından bağımsız —
// snapshot (mesaj bazlı reload) bayat kalabilir. guncelNotify dosyayı TAZE
// okur; dosya bozuksa yedeğe düşer, çökmez. Canlı ölçülen tuzağın testi:
// `to` dosyada tek LID'e düşürülünce snapshot hâlâ eski çift hedefli olsa
// bile bildirim yeni ayarla gitmelidir.
func TestGuncelNotifyDosyadanOkur(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "tetikleyici.yaml")
	// Yedek = BAYAT snapshot: eski çift hedef.
	bayat := notifyConfig{To: stringList{"905550000001", "12345678901234@lid"}, OnStart: true}

	// Dosyada YENİ ayar (yalnız LID).
	yeniDosya := `notify:
  to: ["12345678901234@lid"]
  on_start: true
rules:
- name: kayit
  match: { name: "Mefamex" }
  do: [ { type: command, code: "true" } ]
`
	if err := os.WriteFile(yol, []byte(yeniDosya), 0o600); err != nil {
		t.Fatal(err)
	}

	got := guncelNotify(yol, func() notifyConfig { return bayat })
	if len(got.To) != 1 || got.To[0] != "12345678901234@lid" {
		t.Errorf("dosya taze okunmalı (tek LID), bayat snapshot'a düşüldü: %+v", got.To)
	}
	if !got.OnStart {
		t.Errorf("dosyadaki on_start okunmalı: %+v", got)
	}

	// Dosya bozulursa yedeğe düş, çökme.
	if err := os.WriteFile(yol, []byte(":bozuk: [:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got2 := guncelNotify(yol, func() notifyConfig { return bayat })
	if len(got2.To) != 2 {
		t.Errorf("bozuk dosyada yedeğe düşülmeli (eski çift hedef): %+v", got2.To)
	}
}

// TestNotifyBlockConfigtenOkunur: notify bloğu Gerçek config ayrıştırıcısından
// geçer (KnownFields). Yanlış alan adı → hata; doğru alanlar → okunur.
func TestNotifyBlockConfigtenOkunur(t *testing.T) {
	set, err := parseConfig([]byte(`notify:
  to: ["905550000001", "12345678901234@lid"]
  on_start: true
  on_stop: false
  on_bridge_up: true
  throttle: "5m"
  retry: "30s"
  max_retry: 10
rules:
- name: kayit
  match: { name: "Mefamex" }
  do: [ { type: command, code: "true" } ]`))
	if err != nil {
		t.Fatalf("notify bloğu yüklenemedi: %v", err)
	}

	n := set.Notify
	if len(n.To) != 2 || n.To[1] != "12345678901234@lid" {
		t.Errorf("to yanlış okundu: %+v", n.To)
	}
	if !n.OnStart || n.OnStop || !n.OnBridgeUp || n.OnBridgeDown {
		t.Errorf("bayraklar yanlış: %+v", n)
	}
	if n.Throttle != "5m" || n.Retry != "30s" || n.MaxRetry != 10 {
		t.Errorf("süreler yanlış: %+v", n)
	}
	if !n.enabled() {
		t.Error("açık bayraklar varken enabled() false dönmemeli")
	}
}

// TestNotifyBlockYanlisAlanAdiHataVerir: KnownFields sayesinde yanlış yazılmış
// bir alan sessizce yutulmaz — kullanıcı "on_startt" yazınca hata görür.
func TestNotifyBlockYanlisAlanAdiHataVerir(t *testing.T) {
	_, err := parseConfig([]byte(`notify:
  on_startt: true
rules:
- name: kayit
  match: { name: "Mefamex" }
  do: [ { type: command, code: "true" } ]`))
	if err == nil {
		t.Fatal("bilinmeyen notify alanı hata vermeli (KnownFields)")
	}
	if !strings.Contains(err.Error(), "on_startt") {
		t.Errorf("hata hangi alanın bozuk olduğunu söylemeli: %v", err)
	}
}
