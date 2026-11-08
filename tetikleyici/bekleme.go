// whatsapp-bridge-project/tetikleyici/bekleme.go
//
// RATE LIMIT SONRASI GERİ ÇEKİLME (backoff).
//
// SORUN: ücretsiz model sık tetiklemede `exit 1` veriyor. Bugüne kadar bu
// yalnızca kullanıcıya bildirilen bir HATA idi: "Model şu an yük altında".
// Kullanıcı cevabı alamıyor, tekrar yazıyor, AJAN TEKRAR ÇALIŞIYOR, yine
// başarısız oluyor. Her deneme kotayı daha da bitiriyor.
//
// Yani bildirim vardı ama KORUMA yoktu. Kullanıcı bilgilendiriliyordu,
// sistem aynı şeyi yapmaya devam ediyordu.
//
// ÇÖZÜM: rate limit görüldüğünde o sohbet bir süre bekleme durumuna alınır.
//Bu sürede gelen mesajlar ajana GİTMEZ; kullanıcıya ne olduğu ve ne zaman
//tekrar deneyebileceği söylenir.
//
// ⛔ SESSİZLİK YASAK: Bekleme süresince mesaj sessizce düşerse kullanıcı
// "sistem bozuk" sanır. Kötüye kullanım: kullanıcı kasıtlı olarak kotasını
// tüketmek isteyen biri olabilir; susturmak onu teşvik eder. Bu yüzden her
// mesajda bir kez, kısa ve sebebiyle birlikte bildirilir.

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// rateLimitBekleme, rate limit görüldükten sonra o sohbetin bekleneceği süre.
//
// 10 dakya seçildi: yeterince uzun ki kota toparlansın, kısa değil ki
// kullanıcı "sistem mi bozuk" diye düşünmesin. Ücretsiz katmanda kotalar
// dakikalar içinde tükenip bir saat içinde yenileniyor; 10 dakika ikisinin
// arasında.
const rateLimitBekleme = 10 * time.Minute

// rateLimitBekleme ispat işaretleri.
//
// opencode'nin hata metni sürüme göre değişebiliyor; birkaç biçim aranıyor.
// DİKKAT: "limit" tek başına aranmıyor — "rate limit" bilmeden bir dosya
// yolundaki "limit" sözcüğü kullanıcıya yanlış sebep bildirir.
var rateLimitIsaretleri = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",
	"too many requests",
	"429",
	"quota exceeded",
	"insufficient_quota",
	"kota",
}

// rateLimitMi, hata bir kota/rate limit hatası mı diye söyler.
//
// Kullanıcıya gösterilecek sebebi de üretir: yanlış sebep bildirmek, doğru
// sebebi gizlemekten kötüdür (kullanıcı kendi çözümünü bulamaz).
func rateLimitMi(hata string) (bool, string) {
	kucuk := strings.ToLower(hata)
	for _, isaret := range rateLimitIsaretleri {
		if strings.Contains(kucuk, isaret) {
			return true, "Model şu an yük altında (kota doldu)."
		}
	}
	return false, ""
}

// beklemeDurumu, bir sohbetin bekleme bilgisi.
type beklemeDurumu struct {
	// bitis, beklemenin bittiği an.
	bitis time.Time
	// sonBildirim, kullanıcıya en son ne zaman söylendiği.
	//
	// Her mesajda söylemek spam olurdu. Ama hiç söylememek daha kötü olurdu
	// (sessizlik = sistem bozuk izlenimi). Bir kez söyle, sonra sus.
	sonBildirim time.Time
}

// bekleme, sohbet bazlı bekleme durumlarını tutar.
//
// Neden ayrı yapı: engine'in kendisi zaten kilitli bir yapı ve oraya
// eklenecek alan motorun ana işiyle (kural eşleme) ilgisi yok. Ayrı
// tutmak, kilit çekişmesi ve kapsam karmaşasını azaltır.
type bekleme struct {
	mu     sync.Mutex
	sohbet map[string]beklemeDurumu
	// simdi, testlerin zamanı denetleyebilmesi için. Nil ise time.Now.
	simdi func() time.Time
}

func yeniBekleme() *bekleme {
	return &bekleme{sohbet: make(map[string]beklemeDurumu)}
}

func (b *bekleme) saat() time.Time {
	if b == nil {
		return time.Now()
	}
	if b.simdi != nil {
		return b.simdi()
	}
	return time.Now()
}

// yeniSozYap, bir hatanın rate limit olup olmadığına bakmadan sohbeti
// beklemeye alır. Çağıran zaten sebebi bildireceğini bilir.
func (b *bekleme) baslat(jid string, sure time.Duration) time.Time {
	// Nil alıcı = koruma yok (yalnız test yapıları bu yolu kullanır).
	// Panik atmak, korumanın yokluğundan çok daha kötü.
	if b == nil {
		return time.Time{}
	}
	if sure <= 0 {
		sure = rateLimitBekleme
	}
	bitis := b.saat().Add(sure)

	b.mu.Lock()
	defer b.mu.Unlock()

	// Bekleme ZATEN daha uzunsa uzatma: her yeni hata süreyi baştan
	// başlatmasın, yoksa kullanıcı sürekli hata alır ve hiç çıkamaz.
	// En uzun süre kazanır.
	if mevcut, varMi := b.sohbet[jid]; varMi && mevcut.bitis.After(bitis) {
		return mevcut.bitis
	}

	b.sohbet[jid] = beklemeDurumu{bitis: bitis}
	return bitis
}

// kalan, sohbetin bekleme süresi doldu mu diye söyler. Kalmıyorsa (0) o
// sohbet kullanılabilir.
func (b *bekleme) kalan(jid string) time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	durum, varMi := b.sohbet[jid]
	if !varMi {
		return 0
	}
	kalan := durum.bitis.Sub(b.saat())
	if kalan <= 0 {
		delete(b.sohbet, jid)
		return 0
	}
	return kalan
}

// bildir, kullanıcıya bu bekleme için söylenmeli mi diye söyler.
//
// Kural: bekleme başladıktan sonra İLK mesajda söyle, sonra SUS. Bekleme
// bittikten sonra yeni bir bekleme başlarsa tekrar söylenir (yeni
// bildirim saati sıfırlanır).
func (b *bekleme) bildir(jid string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	durum, varMi := b.sohbet[jid]
	if !varMi {
		return false
	}
	if durum.sonBildirim.IsZero() {
		durum.sonBildirim = b.saat()
		b.sohbet[jid] = durum
		return true
	}
	return false
}

// temizle, süresi dolmuş kayıtları siler.
//
// Sürekli sohbet açılıp kapanırsa harita şişer. Tetikleyici günlerce
// açık kalabildiği için bu bir "sonra hallederiz" değil, yapılması gereken.
func (b *bekleme) temizle() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	simdi := b.saat()
	for jid, durum := range b.sohbet {
		if durum.bitis.Before(simdi) {
			delete(b.sohbet, jid)
		}
	}
}

// beklemeyeYanit, bekleme süresince kullanıcıya ne söyleneceğini üretir.
//
// ⛔ SESSİZ GERİ ÇEKİLME YOK. Mesajı yutmak, sistemi "çökmüş" gösterir.
//
// Metin iki şeyi birden söyler: NE oldu ve NE ZAMAN tekrar deneyebilir.
// Yalnız "biraz sonra" demek yetersizdir — kullanıcı bunu sıralı birkaç kez
// deneyip sonunda vazgeçer.
func beklemeyeYanit(kalan time.Duration) string {
	if kalan <= 0 {
		return ""
	}

	dakika := int(kalan.Minutes())
	if dakika < 1 {
		// "1 dakika" demek yerine saniye ver: çok kısa sürelerde yuvarlama
		// kullanıcıyı yanıltır (60 sn kaldıyken "1 dakika" demek sınırdır).
		return fmt.Sprintf("⏸ Model kotası doldu. %d saniye sonra tekrar deneyebilirsin. "+
			"Bu arada tekrar denersen kota daha da erken dolar.", int(kalan.Seconds()))
	}

	return fmt.Sprintf("⏸ Model kotası doldu, %d dakika bekliyorum. "+
		"Bu arada tekrar denersen kota daha da erken dolar. "+
		"Aynı anda bir iş yapabilirim (/help).", dakika)
}

// beklemeUyarisi, gönderimi dener; köprü yoksa sessizce geçer.
//
// ⛔ opts BOŞ GEÇİLMEZ. `options{}` içinde sendTimeout 0'dır ve
// `context.WithTimeout(ctx, 0)` anında süresi dolmuş bir bağlam demektir:
// gönderim ANINDA başarısız olur, uyarı hiç gitmez ve kullanıcı sebebi
// öğrenemez. Bu tuzak bu dosyaya yazıldı çünkü ilk denemede buraya
// `options{}` verilmişti ve uyarı sessizce kayboluyordu.
//
// Test kipi ve "ajanı çalıştır" kipinde köprü olmayabilir; o zaman uyarı
// gönderilemez ve bu bir HATA DEĞİLDİR. Panik atmak yanlıştır — ama hata da
// gizlenmemelidir, günlüğe yazılır.
func beklemeUyarisi(ctx context.Context, opts options, client *bridgeClient, jid string, kalan time.Duration) {
	metin := beklemeyeYanit(kalan)
	if metin == "" {
		return
	}

	if client == nil {
		logf("  bekleme uyarısı gönderilemedi (köprü yok): %s", metin)
		return
	}

	// opts.sendTimeout 0 ise kendi bütçemizi kullan: aksi hâlde uyarı
	// her gönderimde "anında zaman aşımı" hatasına düşer.
	gonderme := opts
	if gonderme.sendTimeout <= 0 {
		gonderme.sendTimeout = 10 * time.Second
	}

	gonderCtx, iptal := context.WithTimeout(ctx, 20*time.Second)
	defer iptal()

	if err := gonder(gonderCtx, gonderme, client, jid, metin); err != nil {
		logf("  bekleme uyarısı gönderilemedi: %v", err)
		return
	}
	logf("  bekleme uyarısı gönderildi: %s", truncate(metin, 120))
}

// kisaSure, süreyi kısa ve insan okunur biçime çevirir (yalnız günlük için).
//
// Düz "10m0s" yazmak yerine "10dk" demek, günlüğü okuyan kişinin hızını
// artırır. Birim yanlış okunursa hata ayıklamak zorlaşır; bu yüzden
// belirsizlik bırakılmaz: yuvarlama yapılmaz.
func kisaSure(s time.Duration) string {
	if s <= 0 {
		return "0"
	}
	if s >= time.Minute {
		dakika := int(s.Minutes())
		if s%time.Minute != 0 {
			return fmt.Sprintf("%ds %dsn", dakika, int((s % time.Minute).Seconds()))
		}
		return fmt.Sprintf("%d dk", dakika)
	}
	return fmt.Sprintf("%d sn", int(s.Seconds()))
}
