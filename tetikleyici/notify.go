// whatsapp-bridge-project/tetikleyici/notify.go

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Bildirim türleri — damga dosyasının anahtarları ve log etiketleridir.
const (
	bildirimStart     = "start"
	bildirimStop      = "stop"
	bildirimKopruUp   = "bridge_up"
	bildirimKopruDown = "bridge_down"
	bildirimConfig    = "config_change"
)

// Varsayılanlar (TODO'daki şartname):
//
//	throttle: 5m   — aynı tür 5 dakika içinde bir daha gitmez
//	retry: 30s     — köprü bağlı değilse deneme aralığı
//	max_retry: 10  — en fazla deneme, sonra günlüğe yazıp vazgeç
const (
	varsayilanBildirimThrottle = 5 * time.Minute
	varsayilanBildirimRetry    = 30 * time.Second
	varsayilanBildirimMaxRetry = 10
)

// notifier, tetikleyicinin kendi yaşam döngüsü bildirimlerini gönderir.
//
// Kural değildir: mesaj eşleşmesiyle ilgisi yoktur. Tetikleyici açılınca,
// kapanınca, köprü durumu değişince ve config değişince `to` listesindeki
// kimliğe kısa bir metin gider.
//
// Gönderim politikası (spam olmasın diye):
//
//   - Aynı tür bildirim, throttle süresi dolana dek bir daha gitmez.
//     Damgalar DOSYADA tutulur (.tetikleyici/bildirim.txt): tetikleyici
//     yeniden başlasa da hatırlar. Yoksa her restart'ta on_start bir daha
//     giderdi.
//   - Köprü bağlı değilse maxRetry kez, retry aralığıyla denenir; sonra
//     günlüğe yazılıp vazgeçilir. Gönderilemeyeceği belli, ısrar anlamsız.
//   - Başarısız gönderim sahibine hata mesajı GÖNDERMEZ (köprü düştü,
//     cevap gönderecek hâlde değiliz); yalnız günlüğe yazılır.
//
// ⛔ "KENDİ HESABIMIZA ASLA": gönderim yalnız notify.to'daki kimliklere
// gider. Tetikleyici köprünün kendi numarasını bilemez (health yanıtında
// yok), bu yüzden bu koruma config seviyesindedir: `to`'ya kendi numaranı
// yazma. Belge: config.readme.md §notify.
type notifier struct {
	client *bridgeClient
	opts   options

	// cfg, her çağrıda GÜNCEL config'i verir (engine.snapshot). Config
	// mesaj başına okunduğu için bildirim ayarları da dosya kaydedilince
	// geçerli olur; restart gerekmez.
	cfg func() notifyConfig

	damga *damgaDefteri

	// köprü durumu: yalnız GEÇİŞLERDE bildirilir. İlk gözlem sessizdir
	// (tetikleyici başlarken köprü zaten düşükse "düştü" diye bağırmayız).
	kopruMu   sync.Mutex
	kopruOnce bool
	kopruUp   bool
}

// yeniNotifier, bildirim yöneticisi kurar. damgaYolu, throttle damgalarının
// saklandığı dosyadır (ör. .tetikleyici/bildirim.txt).
func yeniNotifier(client *bridgeClient, opts options, cfg func() notifyConfig, damgaYolu string) *notifier {
	if damgaYolu == "" {
		damgaYolu = defaultState("bildirim.txt")
	}
	return &notifier{
		client: client,
		opts:   opts,
		cfg:    cfg,
		damga:  yeniDamgaDefteri(damgaYolu),
	}
}

// baslat, on_start bildirimini gönderir. Yanlışsa (restart'lar arasında
// throttle dolu) damga dosyasına bakılır ve sessizce geçilir.
func (n *notifier) baslat(ctx context.Context) {
	if !n.cfg().OnStart {
		return
	}
	n.bildir(ctx, bildirimStart, "✅ Tetikleyici açıldı — mesaj izleme aktif")
}

// durdur, on_stop bildirimini gönderir.
//
// ⚠️ Süreç kapanmadan ÖNCE çağrılmalı ve bağlamı kısa ömürlü olmalı:
// durdur dönünce tetikleyici çıkar. Köprü ayaktaysa tek deneme yeter;
// düşükse bağlam bütçesi tükenince vazgeçilir (bekleyemeyiz, kapanıyoruz).
func (n *notifier) durdur(ctx context.Context) {
	if !n.cfg().OnStop {
		return
	}
	n.bildir(ctx, bildirimStop, "🚦 Tetikleyici durduruldu")
}

// kopru, köprü sağlık sonucunu işler ve yalnız DURUM DEĞİŞİMİNDE bildirir.
//
// Akış kopması bir köprü düşüşü DEĞİLDİR: köprü SSE'yi her ~30 saniyede
// bilerek kapatır. Gerçek ölçüt /health'tir. İlk gözlem hiçbir şey
// göndermez — "başlangıçta zaten düşüktü" bir olay değildir.
func (n *notifier) kopru(ctx context.Context, hErr error) {
	cfg := n.cfg()
	up := hErr == nil

	n.kopruMu.Lock()
	degisti := n.kopruOnce && n.kopruUp != up
	n.kopruOnce = true
	n.kopruUp = up
	n.kopruMu.Unlock()

	if !degisti {
		return
	}

	if up && cfg.OnBridgeUp {
		n.bildir(ctx, bildirimKopruUp, "🔌 Köprü bağlandı — mesaj izleme aktif")
	}
	if !up && cfg.OnBridgeDown {
		n.bildir(ctx, bildirimKopruDown, "⛔ Köprüye ulaşılamıyor — mesajlar işlenemiyor")
	}
}

// bildir, tek bir bildirimi throttle + retry kurallarıyla gönderir.
//
// Yalnız başarılı gönderim damgalanır: "son gönderim damgası" mantığı
// gereği. Bir tür başarısız kalırsa damga yoktur ve sonraki olay yeniden
// deneyebilir.
func (n *notifier) bildir(ctx context.Context, tur, metin string) {
	cfg := n.cfg()
	if !cfg.enabled() {
		logf("bildirim atlandı (%s): notify yapılandırılmamış", tur)
		return
	}

	throttle := parseSure(cfg.Throttle, varsayilanBildirimThrottle)
	if !n.damga.izinli(tur, throttle, time.Now()) {
		// 5 dakika içinde aynı türden bir daha göndermeyiz. Sessizce
		// geçmek bilinçli: günlüğü de doldurmaya gerek yok.
		return
	}

	retry := parseSure(cfg.Retry, varsayilanBildirimRetry)
	maxRetry := cfg.MaxRetry
	if maxRetry <= 0 {
		maxRetry = varsayilanBildirimMaxRetry
	}

	for deneme := 1; deneme <= maxRetry; deneme++ {
		if ctx.Err() != nil {
			logf("bildirim iptal (%s): %v", tur, ctx.Err())
			return
		}

		tamam := true
		for _, to := range cfg.To {
			if strings.TrimSpace(to) == "" {
				continue
			}
			if err := n.client.sendText(ctx, to, metin); err != nil {
				logf("bildirim gönderilemedi (%s → %s): %v", tur, to, err)
				tamam = false
			}
		}
		if tamam {
			n.damga.kaydet(tur, time.Now())
			logf("bildirim gitti (%s): %s", tur, metin)
			return
		}

		select {
		case <-ctx.Done():
			logf("bildirim iptal (%s): %v", tur, ctx.Err())
			return
		case <-time.After(retry):
		}
	}

	// En fazla maxRetry deneme yapıldı. Köprü hâlâ yanıt vermiyorsa ısrar
	// anlamsız; günlüğe yazıp vazgeçiyoruz. Hata mesajı sahibe GİTMEZ.
	logf("bildirim BAŞARISIZ (%s): %d deneme sonunda vazgeçildi", tur, maxRetry)
}

// damgaDefteri, aynı tür bildirimin ne zaman son gönderildiğini DOSYADA
// tutar. Yeniden başlatmada hatırlar: yoksa her restart'ta aynı bildirim
// (ör. on_start) tekrar giderdi, throttle'ı anlamsız kılardı.
type damgaDefteri struct {
	yol string

	mu  sync.Mutex
	son map[string]int64 // tür → unix zaman
}

// yeniDamgaDefteri, dosyadan varsa damgaları okur. Dosya yoksa boş defterle
// başlanır (ilk açılış).
func yeniDamgaDefteri(yol string) *damgaDefteri {
	d := &damgaDefteri{yol: yol, son: map[string]int64{}}
	raw, err := os.ReadFile(yol)
	if err != nil {
		return d
	}
	// JSON'dan çözülemezse (yarım yazım, elle bozulma) boş başlanır:
	// bir bildirim eksik gitse de hiç gitmemesinden iyidir. Doğru değilse
	// bir sonraki kayıtta üzerine yazılır.
	_ = jsonUnmarshal(raw, &d.son)
	if d.son == nil {
		d.son = map[string]int64{}
	}
	return d
}

// izinli, tür için throttle eşiği dolmuş mu. Damga yoksa ilk gönderim
// her zaman izinlidir.
func (d *damgaDefteri) izinli(tur string, esik time.Duration, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	son, varMi := d.son[tur]
	if !varMi {
		return true
	}
	return now.Sub(time.Unix(son, 0)) >= esik
}

// kaydet, türü şimdi damgalar ve dosyaya yazar.
func (d *damgaDefteri) kaydet(tur string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.son[tur] = now.Unix()

	raw, err := jsonMarshal(d.son)
	if err != nil {
		logf("damga yazılamadı (%s): %v", tur, err)
		return
	}

	// Yarı yazılmış dosya bırakmamak için önce geçici dosyaya, sonra
	// atomik taşıma. Bu küçük dosya sık yazılmaz; maliyet önemsiz.
	tmp := d.yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(raw), 0o600); err != nil {
		logf("damga yazılamadı (%s): %v", tur, err)
		return
	}
	if err := os.Rename(tmp, d.yol); err != nil {
		logf("damga taşınamadı (%s): %v", tur, err)
	}
}

// guncelNotify, notify ayarlarını DOĞRUDAN config dosyasından taze okur;
// okunamazsa yedek (son snapshot) döner.
//
// Neden: notifier olayları mesaj akışından BAĞIMSIZ çalışır, ama
// engine.snapshot() yalnız MESAJ geldiğinde yenilenir. Config değişince
// watcher olay üretir — o anda snapshot hâlâ bayattır → ESKİ ayarla
// gönderilir. Canlı ölçüldü: `to` tek LID'e düşürüldükten sonra çalışan
// süreç eski çift hedefle kaldı, sonraki config bildirimi yine 2 kanala
// gitti. Olaylar throttle'lı ve seyrek, dosya küçük — taze okumak bedava.
func guncelNotify(yol string, yedek func() notifyConfig) notifyConfig {
	set, err := loadConfig(yol)
	if err != nil {
		return yedek()
	}
	return set.Notify
}

// configDegisikliginiIzle, config dosyasının damgasını periyodik yoklayıp
// değişince bildirir. İlk gözlem sessizdir (startup'ta dosya "yeni
// değişmiş" sayılmaz).
//
// Neden gerekiyor: config mesaj başına yeniden okunur ama kimse değişimi
// BİLDİRMEZ. Kullanıcı dosyayı düzenlediğinde kural değişir — bunu sahibin
// de bilmesi iyi olur. Throttle (5m) sık kaydetmelerde gürültüyü keser.
func configDegisikliginiIzle(ctx context.Context, configPath string, periyot time.Duration, acik func() bool, bildir func()) {
	son := dosyaDamgasi(configPath)

	ticker := time.NewTicker(periyot)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			simdi := dosyaDamgasi(configPath)
			if simdi == "" || simdi == son {
				continue
			}
			son = simdi
			if acik() {
				bildir()
			}
		}
	}
}

// dosyaDamgasi, config değişimini kabaca yakalayan bir imzadır: yol +
// boyut + mtime. Sık kaydetmelerde mtime çözünürlüğü 1 saniyeye takılabilir;
// boyut da eklenince yanlış negatif ihtimali düşer. Yeterince ayırt edici:
// amacımız "değişti" bilgisi, mikro farklar değil.
func dosyaDamgasi(yol string) string {
	st, err := os.Stat(yol)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s|%d|%d", yol, st.Size(), st.ModTime().UnixNano())
}
