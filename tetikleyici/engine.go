// whatsapp-bridge-project/tetikleyici/engine.go

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// engine, config dosyasını canlı tutar ve mesajları kurallarla eşleştirir.
//
// Temel fikir: config bir kez okunmaz, **her mesajda yeniden okunur**. Kullanıcı
// dosyayı düzenleyip kaydettiği anda yeni kural geçerlidir; yeniden başlatma
// gerekmez. Bozuk bir düzenleme de kuralları götürmez — son geçerli config
// korunur.
type engine struct {
	configPath string
	opts       options

	// window, eyleme kaç mesaj bağlam verileceği (0 = yalnız tetikleyen).
	window int

	// Config'den gelen seçenekler
	okunduIsaretle bool
	aiEtiEkle      bool
	debugCikti     bool

	mu     sync.Mutex
	last   ruleSet
	lastOK bool
	// reported, aynı hatayı her mesajda değil bir kez bildirmek tutulur.
	reported string

	// probeMu, aynı anda tek kurtarma taraması çalışmasını sağlar.
	probeMu sync.Mutex
	// reportedProbe, hangi sohbette zaman aşımı yaşandığını tutar; aynı
	// sohbet için her 30 saniyede uyarı basmamak adına.
	reportedProbe string

	// bekleme, rate limit sonrası sohbet bazlı geri çekilme.
	//
	// Neden var: rate limit kullanıcıya bildiriliyordu ama sistem aynı
	// şeyi yapmaya devam ediyordu. Her "tekrar yaz" yeni bir başarısız
	// çağrı = kotayı daha da bitirmek. Bekleme.go'ya bak.
	bekleme *bekleme

	// supheliBellek, aynı numaradan şüpheli uyarıların aralığını tutar
	// (supheli.go). Config her mesajda yeniden okunduğu için özellik
	// aç/kapa restart'sız yansır; yalnız aralık sayaçları burada yaşar.
	supheliBellek *supheliIzleyici
}

// newEngine, verilen config yoluna bağlı bir motor kurar. Hemen okumaz.
func newEngine(configPath string, opts options) *engine {
	return &engine{
		configPath:    configPath,
		opts:          opts,
		bekleme:       yeniBekleme(),
		supheliBellek: yeniSupheliIzleyici(),
	}
}

// zamanlanmisAcikDurum, çalışma anındaki zamanlanmış mesaj bayrağını
// iş parçacığı güvenli döndürür (rules() e.mu altında yazar). Tarayıcı
// goroutine'i değeri bu kapıdan okur — rules() tazelediği için
// /zamanla aç|kapat restart'sız yansır (MEVZU Aşama 3).
func (e *engine) zamanlanmisAcikDurum() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.opts.zamanlanmisAcik
}

// rules, config'i okur ve son geçerli hâli döndürür.
func (e *engine) rules() ruleSet {
	e.mu.Lock()
	defer e.mu.Unlock()

	set, err := loadConfig(e.configPath)
	if err != nil {
		if e.reported != err.Error() {
			e.reported = err.Error()
			logf("config okunamadı, son geçerli kurallar korunuyor: %v", err)
		}
		return e.last
	}

	if !e.lastOK || e.reported != "" {
		logf("config yüklendi: %d kural (%s)", len(set.Rules), e.configPath)
	}
	e.last = set
	e.lastOK = true
	e.reported = ""

	// opencode yolu her okumada tazelenir; kullanıcı config'de yolu sonradan
	// düzeltebilir.
	e.opts.opencode = set.OpenCode

	// MEVZU Aşama 3 (ADIM 1, 2026-10-06): çalışma anı ayar snapshot'ı.
	// rules() HER tetiklemede config'i okuduğu için burada tazelenen değer,
	// config'de `zamanlanmis_mesaj.acik` değiştirilince RESTART'SIZ sonraki
	// tetiklemeye yansır. Öncesinde main.go başlangıçta bir kez set ediyordu
	// ve değişiklik ancak restart'ta görünüyordu.
	e.opts.zamanlanmisAcik = set.Zamanlanmis.enabled()

	// Config seçeneklerini güncelle
	e.okunduIsaretle = set.OkunduIsaretle
	e.aiEtiEkle = set.AIEtiEkle
	e.debugCikti = set.DebugCikti

	return set
}

// snapshot, diske bakmadan son bilinen config'i döndürür. Catch-up gibi
// döngüde tekrar tekrar okuma istemeyen çağıranlar için.
func (e *engine) snapshot() ruleSet {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last
}

// describeRules, aktif kuralları tek satırda özetler.
func describeRules(set ruleSet) string {
	if len(set.Rules) == 0 {
		return "(no rules)"
	}

	parts := make([]string, 0, len(set.Rules))
	for _, r := range set.Rules {
		parts = append(parts, fmt.Sprintf("%s[%s]", r.name(), r.Match.describe()))
	}
	return strings.Join(parts, " · ")
}

// dispatch, mesajı kurallarla eşleştirip tutanların eylemlerini çalıştırır.
//
// Dönüş değeri çalışan kural sayısıdır (günlük için). Tıkanma, hatayı yutarak
// akışı canlı tutar: tek bir eylemin çökmesi diğerlerini iptal etmemelidir.
func (e *engine) dispatch(ctx context.Context, client *bridgeClient, seen *seenStore, m message) int {
	set := e.rules()

	hits := matchingRules(set, m)
	if len(hits) == 0 {
		// Tehlikeli mesaj uyarısı: eşleşmeyen mesajı YANITLAMAZ, yalnız
		// tanınmayan şüpheli kaynakları admin'e bildirir (supheli.go).
		// "Kapalı hat sessizdir" korunur: gelen numaraya hiçbir şey
		// gönderilmez, uyarı yalnız admin'e gider.
		e.supheliUyari(ctx, client, set, m)
		return 0
	}
	if seen.alreadyHandled(m.ID) {
		return 0
	}

	// okundu_isaretle: gelen mesajı okundu işaretle (mavi tik)
	if e.okunduIsaretle && client != nil {
		markCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := client.markRead(markCtx, m.From, []string{m.ID}); err != nil {
			logf("  okundu işaretleme hatası: %v", err)
		} else {
			logf("  okundu işaretlendi (mavi tik)")
		}
		cancel()
	}

	// ⛔ RATE LIMIT GERİ ÇEKİLMESİ: bu sohbet bekleme süresindeyse ajan
	// ÇALIŞTIRILMAZ. Kullanıcıya ne olduğu ve ne zaman deneyebileceği
	// söylenir — sessizce yutmak sistemi "çökmüş" gösterir.
	//
	// Kontrol YAKALAMA'DAN ÖNCE yapılır: bağlam penceresi için köprüye
	// gitmek de gereksiz; zaten cevap verilmeyecek.
	if kalan := e.bekleme.kalan(m.From); kalan > 0 {
		if e.bekleme.bildir(m.From) {
			logf("  bekleme sürüyor (%s), ajan çalıştırılmıyor: %s", kisaSure(kalan), truncate(m.readableText(), 80))
			beklemeUyarisi(ctx, e.opts, client, m.From, kalan)
		} else {
			logf("  bekleme sürüyor (%s), ajan çalıştırılmıyor (zaten söylendi)", kisaSure(kalan))
		}
		// Kural "işlendi" sayılır: aksi hâlde mesaj tekrar denemede aynı
		// kontrolü tetikler ve aynı uyarı tekrar tekrar gider.
		seen.mark(m.ID)
		return len(hits)
	}

	p := e.buildPayload(ctx, client, m)

	names := make([]string, 0, len(hits))
	for _, r := range hits {
		names = append(names, r.name())
	}
	logf("EŞLEŞTİ: %s (%s) — %d kural: %s",
		m.label(), truncate(m.From, 40), len(hits), strings.Join(names, ", "))

	// Her kural için kimliğin nasıl sağlandığını yaz: numara mı tuttu, isim mi?
	// LID sohbetlerinde numaranın alanda olmaması normaldir; günlük bunu
	// açıkça söylemezse kullanıcı filtresinin çalışmadığını sanır.
	for _, r := range hits {
		logf("  %s → %s", r.name(), r.Match.resolve(m).detailLine())
	}

	logf("  metin: %s", truncate(m.readableText(), 120))

	// Kural tetiklendiğini işaretlemeden önce bağlam hazırlanır; bağlam
	// alınamazsa mesaj tekrar denensin diye işaretlenmez.
	seen.mark(m.ID)

	if e.opts.dryRun {
		for _, r := range hits {
			for _, a := range r.Actions {
				logf("  KURU MOD — %s → %s", r.name(), a.label())
			}
		}
		return len(hits)
	}

	// Paralel eylem çalıştırma: her action ayrı goroutine.
	// Session kilidi (oturumKilidiAl) runAction içinde — aynı session
	// kullanan action'lar kendiliğinden sıralanır, farklı session'lar
	// paralelde çalışır. sync.WaitGroup ile hepsi bitene kadar bekleriz.
	var wg sync.WaitGroup
	for _, r := range hits {
		for _, a := range r.Actions {
			wg.Add(1)
			go func(ruleName string, act action) {
				defer wg.Done()

				logf("  eylem: %s → %s", ruleName, act.label())

				reply, err := runAction(ctx, e.opts, act, p, client, e.aiEtiEkle, e.debugCikti)
				if err != nil {
					logf("  eylem BAŞARISIZ: %s → %s: %v", ruleName, act.label(), err)

					// Rate limit korumasını KUR: aynı sohbette bir daha
					// denemek kotayı bitirir. Kullanıcıya zaten bildirildi
					// (bosCiktiSebebi); burada ayrıca sistem kendini durdurur.
					//
					// Bu iki ayrı şeydir ve ikisi de gerekir: kullanıcıya
					// söylemek yeterli DEĞİL — söyledikten sonra da
					// denemeye devam ediyorduk.
					if kotali, _ := rateLimitMi(err.Error()); kotali {
						bitis := e.bekleme.baslat(m.From, rateLimitBekleme)
						logf("  ⏸ rate limit → %s bekleme: %s", m.From, kisaSure(time.Until(bitis)))
					}
					return
				}
				if reply != "" {
					// Yanıt ne kadar uzunsa WhatsApp o kadar kabul eder; taşmayı
					// sessizce kesmek yerine gönderilenin ne olduğunu yaz.
					logf("  yanıt gönderildi (%d karakter): %s", len(reply), truncate(reply, 200))
				}
			}(r.name(), a)
		}
	}
	wg.Wait()

	logf("  tamam (%d kural çalıştı)", len(hits))
	return len(hits)
}

// buildPayload, tetikleyen mesajı ve geçmişini toplar.
//
// Geçmiş alınamazsa tek mesajla devam edilir: eylem çalışsın, kullanıcı sorunu
// günlükten görsün. Akış her zaman eskiden yeniye sıralıdır.
func (e *engine) buildPayload(ctx context.Context, client *bridgeClient, trigger message) payload {
	var history []message

	if e.window > 0 && client != nil {
		fetchCtx, cancel := context.WithTimeout(ctx, e.opts.fetchTimeout)
		fetched, err := client.history(fetchCtx, trigger.From, e.window)
		cancel()

		if err != nil {
			logf("geçmiş alınamadı (%s): %v", trigger.From, err)
		} else {
			trigger = keepTrigger(fetched, trigger)
			history = fetched
		}
	}

	history = withTrigger(history, trigger)
	return payload{
		Trigger:    trigger,
		History:    sortOldestFirst(history),
		ConfigPath: e.configPath,
	}
}

// withTrigger, geçmişte tetikleyen mesaj yoksa başa ekler.
func withTrigger(history []message, trigger message) []message {
	for _, m := range history {
		if m.ID == trigger.ID {
			return history
		}
	}
	return append([]message{trigger}, history...)
}

// keepTrigger, geçmişte bulunan tetikleyen mesajın dolu sürümünü seçer.
//
// Köprü geçmişten gelen mesajda push_name veya metni doldurmayabilir; canlı
// akıştaki sürüm daha eksiksiz olur, o yüzden canlı sürüm tercih edilir ama
// geçmişteki daha zengin alanlar boşluk doldurmak için alınır.
func keepTrigger(fromHistory []message, live message) message {
	for _, m := range fromHistory {
		if m.ID != live.ID {
			continue
		}
		if live.PushName == "" {
			live.PushName = m.PushName
		}
		if live.Text == "" {
			live.Text = m.Text
		}
		return live
	}
	return live
}

// sortOldestFirst, mesajları eskiden yeniye dizer.
func sortOldestFirst(msgs []message) []message {
	sorted := make([]message, len(msgs))
	copy(sorted, msgs)

	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Timestamp < sorted[j-1].Timestamp; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted
}

// catchUp, akış kopukken gelen mesajları kurtarır.
//
// Köprü SSE'yi her ~30 saniyede kapatır; bu pencerede gelen mesaj canlı akışta
// görünmez. Bağlantı yeniden kurulunca bilinen sohbetlerin son penceresi
// taranır ve görülmemiş mesajlar aynı kurallardan geçirilir.
func catchUp(ctx context.Context, e *engine, client *bridgeClient, seen *seenStore, chats []string) error {
	if len(chats) == 0 {
		return nil
	}

	// Aynı anda tek tarama olsun: her yeniden bağlantıda üst üste binen
	// taramalar geçmiş isteklerini çoğaltır ve hiçbirini bitiremez.
	if !e.probeMu.TryLock() {
		logf("kurtarma taraması hâlâ sürüyor, bu tur atlandı")
		return nil
	}
	defer e.probeMu.Unlock()

	for _, jid := range chats {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Sohbet başına bütçe. Köprü tamponu yetersizse WhatsApp'a sorup
		// 10 saniye bekler, o yüzden bütçe bunun üstünde olmalı; aksi hâlde
		// tarama her sohbette zaman aşımına uğrar ve hiçbir mesaj yakalanmaz.
		fetchCtx, cancel := context.WithTimeout(ctx, e.opts.probeTimeout)
		window, err := client.history(fetchCtx, jid, probeWidth(e.window))
		cancel()

		if err != nil {
			// "geçmiş boş" sıradandır: o sohbette ilgilendiğimiz mesaj yok.
			// Zaman aşımı ise gerçek bir sorundur, ama her 30 saniyede tekrar
			// yazmak günlüğü boğar; yalnız durum değişince yaz.
			switch {
			case strings.Contains(err.Error(), "geçmiş boş"):
				// sessiz
			case strings.Contains(err.Error(), "deadline exceeded"):
				if e.reportedProbe != jid {
					e.reportedProbe = jid
					logf("catch-up zaman aşımı (%s): köprü geçmişi getiremiyor, "+
						"bu sohbetin mesajları kaçabilir", jid)
				}
			default:
				logf("catch-up atlandı (%s): %v", jid, err)
			}
			continue
		}

		// Zaman aşımı yaşanmamışsa uyarıyı temizle.
		e.reportedProbe = ""

		// Köprüden istenenden az mesaj gelirse uyar: köprü WhatsApp'tan
		// geçmiş çekememiş olabilir (topUpFromWhatsApp 0 döndürmüş).
		istenen := probeWidth(e.window)
		if len(window) < istenen/2 && len(window) > 0 {
			logf("⚠️ catch-up: %s için %d istenen, %d geldi — köprü geçmişi eksik olabilir",
				jid, istenen, len(window))
		}

		for _, m := range window {
			if m.fromMe() || seen.alreadyHandled(m.ID) {
				continue
			}
			// Filtre önce: eşleşmeyen mesaj "kalmış mesaj" değildir, yalnızca
			// ilgisizdir. Önce loglamak günlüğü yanıltıcı dolduruyordu.
			if len(matchingRules(e.rules(), m)) == 0 {
				continue
			}

			// ⏱️ CATCH-UP YAŞ FİLTRESİ:
			// Tetikleyici uzun süre kapalı kaldıysa geçmişteki bayat mesajları
			// yeniden canlandırmak arka arkaya spam cevaplar üretir (2026-10-14 vakası).
			// maxAge'den eski mesajlar dispatch edilmez, görüldü işaretlenir ve atlanır.
			maxAge := e.opts.catchUpMaxAge
			if maxAge <= 0 {
				maxAge = 10 * time.Minute
			}
			if m.Timestamp > 0 {
				ts := m.Timestamp
				if ts > 1e11 {
					ts = ts / 1000
				}
				mesajZamani := time.Unix(ts, 0)
				yas := time.Since(mesajZamani)
				if yas > maxAge {
					seen.mark(m.ID)
					logf("catch-up atlandı (bayat mesaj, %s önce): %s [%s]",
						kisaSure(yas), jid, m.ID)
					continue
				}
			}

			logf("YAKALAMA: kopuk pencerede kalmış mesaj (%s)", jid)
			e.dispatch(ctx, client, seen, m)
		}
	}

	return nil
}

// probeWidth, catch-up taramasının kaç mesaj bakacağını belirler.
//
// Tek mesaj sormak yetmez: köprü mesajları sırasız dönebiliyor ve en yenisi
// değil bayat olanı gösterebiliyor. Geniş pencere hem yeterli güvenlik
// hem de tampon yeterliyse bellekten gelir (WhatsApp'a gitmeden).
// Köprü maxHistoryCount=100 sınırı var; 100 isteyip az gelirse köprü
// WhatsApp'tan geçmiş çekmeyi dener (topUpFromWhatsApp).
func probeWidth(window int) int {
	const min = 50
	if window < min {
		return min
	}
	if window > 100 {
		return 100
	}
	return window
}
