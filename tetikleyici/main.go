// whatsapp-bridge-project/tetikleyici/main.go

// Command tetikleyici, WhatsApp mesajlarını izler ve config dosyasındaki
// kurallara uyanları çalıştırır.
//
// Kullanım:
//
//	tetikleyici --config tetikleyici.yaml
//
// Config, mesaj başına yeniden okunur: dosyayı düzenleyip kaydetmek yeterlidir,
// yeniden başlatma gerekmez. Tek kural yoktur — istediğin kadar kural yazılır,
// hepsi de tutarsa hepsi çalışır.
//
// Bağımsızdır: köprüyle yalnızca HTTP/JSON üzerinden konuşur, bridge-go
// paketlerine bağlı değildir.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// options, çalışma ayarlarıdır. Kural davranışı değil; config'den gelir.
type options struct {
	// configPath, config dosyasının yoludur; proje kökü (= .tetikleyici
	// state dizini) ondan türetilir (/reply bekleyen cevaplar dosyası).
	configPath string

	// opencode, session eylemlerinin çağıracağı binary. Config'den okunur.
	opencode string

	// actionTimeout tek bir eylemin üst sınırı.
	actionTimeout time.Duration

	// fetchTimeout, eyleme başlamadan önce geçmiş çekme süresi.
	fetchTimeout time.Duration

	// probeTimeout, catch-up'ın sohbet başına bütçesi.
	probeTimeout time.Duration

	// catchUpMaxAge, bu süreden eski mesajların catch-up sırasında atlanmasını sağlar.
	// Varsayılan: 10 dakika.
	catchUpMaxAge time.Duration

	// sendTimeout, WhatsApp'a yanıt gönderme süresi.
	sendTimeout time.Duration

	// devamAra, rate limit sonrası otomatik devam turları arasındaki nefes.
	// run() bunu rateLimitDevamAra ile kurar; testler 0 verir (bekleme yok).
	// 0 = ara yok.
	devamAra time.Duration

	// zamanlanmisAcik, config'deki zamanlanmis_mesaj.acik bayrağıdır.
	// Kapalıyken /zamanla komutu reddedilir ve tarayıcı başlamaz.
	zamanlanmisAcik bool

	// dryRun eylemleri çalıştırmaz, ne yapılacağını yalnız yazar.
	dryRun bool
}

// args, komut satırının tamamıdır.
type args struct {
	configPath string
	bridge     string
	seenFile   string
	logFile    string
	window     int
	dryRun     bool
	logOnly    bool

	// actionTO tek bir eylemin üst sınırı; opencode turları uzun sürebilir.
	actionTO time.Duration
}

func main() {
	raw := parseArgs()

	if err := openLog(raw.logFile, raw.logOnly); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, raw); err != nil {
		logf("hata: %v", err)
		os.Exit(1)
	}
	logf("kapatılıyor")
}

// run, süreci kurar ve ana döngüyü çalıştırır.
func run(ctx context.Context, raw args) error {
	opts := options{
		configPath: raw.configPath,
		// opencode'ye giden istek bir LLM turu olabilir; 3 dakika çoğu soru
		// için yeterli ama uzun araştırmalarda dar kalıyor. Bayrakla
		// büyütülebiliyor (--action-timeout 5m).
		actionTimeout: raw.actionTO,
		fetchTimeout:  20 * time.Second,
		// Köprü tamponu yetersizse WhatsApp'a sorup 10 saniye bekliyor; bütçe
		// bunun üstünde olmalı. Eskiden 8 saniyeydi ve tarama her sohbette
		// zaman aşımına uğrayıp hiçbir mesaj yakalayamıyordu.
		probeTimeout:  20 * time.Second,
		catchUpMaxAge: 10 * time.Minute,
		sendTimeout:   15 * time.Second,
		// Rate limit sonrası otomatik devam turları arası nefes (devamDene).
		devamAra: rateLimitDevamAra,
		dryRun:   raw.dryRun,
	}

	if _, err := loadConfig(raw.configPath); err != nil {
		return err
	}

	client := newBridgeClient(raw.bridge, 15*time.Second)
	engine := newEngine(raw.configPath, opts)
	engine.window = raw.window
	seen := newSeenStore(raw.seenFile)

	set := engine.rules()
	if len(set.Rules) == 0 {
		logf("uyarı: config'de kural yok, hiçbir şey tetiklenmeyecek (%s)", raw.configPath)
	}
	// opencode yolu: engine her mesajda tazeler ama heartbeat startup
	// kopyasını kullanır — ilk turda boş kalmasın diye burada da doldur.
	// (run döngüsü her turda yine tazeler.)
	opts.opencode = set.OpenCode
	if set.CatchUpMaxAge != "" {
		if d, err := time.ParseDuration(set.CatchUpMaxAge); err == nil && d > 0 {
			opts.catchUpMaxAge = d
		}
	}
	engine.opts = opts

	logf("tetikleyici başladı")
	logf("  köprü   : %s", raw.bridge)
	logf("  config  : %s", raw.configPath)
	logf("  kurallar: %d → %s", len(set.Rules), describeRules(set))
	logf("  catch-up: max-age %s", kisaSure(opts.catchUpMaxAge))
	if set.OpenCode != "" {
		logf("  opencode: %s", expandHome(set.OpenCode))
	}
	logf("  dedup   : %s", raw.seenFile)
	if raw.dryRun {
		logf("  KURU MOD: hiçbir eylem çalıştırılmayacak")
	}

	// İzleme listesini config'den doldur: canlı trafik görülmeden önce de
	// kopuk pencerede gelen mesajları yakalayabilmek için.
	chats := newChatWatch()
	seeds := seedJIDs(set)
	for _, jid := range seeds {
		chats.remember(jid)
	}
	if len(seeds) > 0 {
		logf("  izlenen : %s", strings.Join(seeds, ", "))
	}

	// LID sohbetleri numaradan türetilemez, o yüzden isimle sınırlı bir kural
	// başlangıçta o sohbetleri kapsayamaz. Sessizce eksik kalmak yerine
	// söylemek gerekir: kullanıcı aksi hâlde neden tetiklenmedi diye arar.
	if needsExplicitLID(set) {
		logf("  not: LID'li sohbetler numaradan türetilemez. O sohbetler ancak")
		logf("       canlı trafikle öğrenilir ya da config'te tam kimlikle yazılır:")
		logf("       number: [905551234567, 12345678901234@lid]")
	}

	// Kalp atışı: sessiz kalan sohbeti uyarır. Yapılandırılmamışsa hiç
	// başlamaz. Ana akıştan bağımsız çalışır — bir sohbet sessizse o
	// sohbetin kuralı çalışmasa bile uyarı üretilir.
	if hb := newHeartbeat(set, client, opts); hb != nil {
		logf("  kalp atışı: %d sn'de bir, %d dk sessizlikte uyarır",
			int(hb.interval.Seconds()), int(hb.idle.Minutes()))
		go hb.run(ctx, engine)
	}

	// Zamanlanmış mesajlar: "/zamanla <süre> <metin>" ile ertelenen gönderimler.
	// Kapalıysa (zamanlanmis_mesaj.acik yok/false — MEVZU varsayılanı) hiç
	// başlamaz. Açıkken iş dizinini tarar, vadesi geleni köprüye gönderir,
	// gönderileni siler. Köprü düşse bile süreç yaşar — denemeler 1 dk nefesle,
	// 3 başarısızda iptal (zamanlanmis.go).
	//
	// Bayrak engine.opts'a yazılır: runCommand, komut env'ini engine'in
	// kopyasından kurar (engine.go:194 runAction(ctx, e.opts, …)) — yerel
	// opts'a yazmak komut sürecine ulaşmazdı.
	engine.opts.zamanlanmisAcik = set.Zamanlanmis.enabled()
	if engine.opts.zamanlanmisAcik {
		logf("  zamanlanmış mesaj: açık, iş dizini %s", zamanlanmisDizin())
	}
	// Tarayıcı HER ZAMAN başlar (MEVZU Aşama 3 ADIM 3): kapalıyken tur
	// atmaz; /zamanla aç çalışma anında canlandırır. Önceden yalnız açık
	// başlangıçta başlıyordu — çalışma anında açılan işler hiç gönderilmezdi.
	go zamanlanmisIzle(ctx, client, engine.zamanlanmisAcikDurum)

	// Bildirimler (notify): tetikleyicinin kendi olayları — açılış, kapanış,
	// köprü düşüşü/kalkışı, config değişimi. Yapılandırılmışsa on_start
	// şimdi gider; köprü durumu aşağıdaki döngüde izlenir.
	//
	// cfg her bildirimde config dosyasını TAZE okur (guncelNotify) — mesaj
	// bazlı snapshot bayat kalabileceği için event'ler dosyaya göre karar
	// verir. Canlı ölçülen tuzak: eski to listesiyle ikinci kez çift gönderim.
	n := yeniNotifier(client, opts,
		func() notifyConfig {
			return guncelNotify(raw.configPath, func() notifyConfig { return engine.snapshot().Notify })
		},
		defaultState("bildirim.txt"))
	if set.Notify.enabled() {
		logf("  notify  : %s (throttle %s, azami %d deneme)",
			strings.Join(set.Notify.To, ", "),
			parseSure(set.Notify.Throttle, varsayilanBildirimThrottle),
			max(set.Notify.MaxRetry, varsayilanBildirimMaxRetry))
		n.baslat(ctx)

		// Config değişimi: config mesaj başına yeniden okunur ama değişimi
		// kimse bildirmez. Dosya damgası değişince sahibe haber verilir.
		if set.Notify.OnConfigChange {
			go configDegisikliginiIzle(ctx, raw.configPath, 5*time.Second,
				func() bool { return engine.snapshot().Notify.OnConfigChange },
				func() { n.bildir(ctx, bildirimConfig, "⚙️ Config değişti — yeni ayarlar aktif") })
		}
	}

	backoff := 2 * time.Second
	for ctx.Err() == nil {
		// Köprü sağlığı önce: düşüş/kalkış bildirimleri yalnız BUNDAN
		// çıkarılır. Akış kopması (SSE ~30 sn'de bir kapanır) köprü düşüşü
		// DEĞİLDİR; sağlık ise gerçek ölçüttür. İlk gözlem sessizdir.
		hErr := client.health(ctx)
		n.kopru(ctx, hErr)
		if hErr != nil {
			logf("köprüye ulaşılamadı (%v), %.0fs sonra tekrar deneniyor", hErr, backoff.Seconds())
			if !waitFor(ctx, backoff) {
				break
			}
			if backoff < 8*time.Second {
				backoff *= 2
			} else {
				backoff = 2 * time.Second
			}
			continue
		}

		err := connectOnce(ctx, client, engine, seen, chats)
		if err == nil {
			err = errors.New("akış kapandı")
		}

		if ctx.Err() != nil {
			break
		}

		logf("akış koptu (%v), %.0fs sonra yeniden bağlanılıyor", err, backoff.Seconds())

		if !waitFor(ctx, backoff) {
			break
		}

		// 2s → 4s → 8s → 8s... Sonra sıfırlanır, çünkü köprü 30 saniyede bir
		// kopuyor ve her seferinde sabit beklemiyoruz.
		if backoff < 8*time.Second {
			backoff *= 2
		} else {
			backoff = 2 * time.Second
		}
	}

	// on_stop: süreç kapanıyor, bu son şans. Bağlam kısa ömürlü olmalı —
	// durdur dönünce tetikleyici çıkar; köprü düşükse bütçe tükenip
	// vazgeçilir.
	stopCtx, stopIptal := context.WithTimeout(context.Background(), 25*time.Second)
	n.durdur(stopCtx)
	stopIptal()

	return nil
}

// connectOnce, tek bir bağlantıyı tutar. Kopunca hata döner.
//
// Sıralama önemli: **önce canlı akış açılır, sonra kurtarma taraması arka planda
// başlar.** Tersi yapılırsa tarama (sohbet başına saniyeler sürebilir) canlı
// akışı geciktirir ve gecikmenin kendisi yeni bir kör pencere üretir.
func connectOnce(ctx context.Context, client *bridgeClient, engine *engine, seen *seenStore, chats *chatWatch) error {
	if err := client.health(ctx); err != nil {
		return err
	}
	logf("akış bağlandı, tetikleme aktif")

	// Arka plandaki tarama için bağlam: akış kapansa da sürebilsin.
	catchUpCtx, cancelCatchUp := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelCatchUp()

	go func() {
		if err := catchUp(catchUpCtx, engine, client, seen, chats.known()); err != nil {
			logf("kurtarma taraması hatası: %v", err)
		}
	}()

	// Canlı eylemlerin bağlamı akıştan AYRILMALIDIR.
	//
	// Ölçülen hata: "signal: killed" 11 kez, çalışma süreleri 1s-58s.
	// dispatch, streamEvents'in okuma döngüsü içinde senkron çağrılıyordu.
	// Yani bir eylem çalışırken akış OKUNMUYORDU; bağlantı bozulunca
	// akış geri döner ve o eylemin bağlamı iptal olup süreç öldürülüyordu.
	// Kullanıcının şikâyeti bu zincirin sonu: "uzun işlerde cevap
	// WhatsApp'a gitmiyor".
	//
	// WithoutCancel yalnız akış KOPMASINI bağlamdan soyutlar. Süreç kapanışı
	// (SIGTERM) yine de iptal eder: kapatmada yarım kalan iş olur, ama
	// kullanıcıya artık sebep bildirimi gider (teslim.go).
	workCtx := context.WithoutCancel(ctx)

	// Okuma ile işlemeyi ayır.
	//
	// Önceden bir eylem çalışırken okuma döngüsü duruyordu. 12 dakikalık
	// İŞLEYİCİ (2026-10-06): kuyruk artık TEK işçili değil.
	//
	// Sorun: tek işçide 12 dakikalık bir iş, o süre boyunca gelen TÜM
	// mesajları — başka sohbetler dâhil — bekletiyordu. Kullanıcı "hiç cevap
	// yok" diyordu.
	//
	// Şimdi: her sohbet kendi işçisinde işlenir (farklı sohbetler paralel),
	// ama aynı sohbet SIRA korur (iki cevap birbirine giremez) ve aynı
	// opencode oturumu oturum kilidiyle korunur (paralel yazı olmaz).
	//
	// Akış okuma işi hiçbir zaman bekletmez: mesajlar işçi kanallarına
	// düşer ve akış devam eder.
	isleyici := yeniIsleyici(func(ctx context.Context, m message) {
		engine.dispatch(ctx, client, seen, m)
	}, varsayilanIsleyiciAyarlari())

	defer func() {
		// Bağlantı kapandı: bekleyen işleri bitir, işçileri çek.
		isleyici.kapat(2 * time.Minute)
		logf("işleyici durduruldu (%s)", isleyici.ozet())
	}()

	return client.streamEvents(ctx, func(m message) {
		if m.fromMe() {
			return
		}
		chats.remember(m.From)

		if !isleyici.besle(workCtx, m) {
			// Süreç kapanıyor ya da düzen dolu: mesaj köprüde kalır,
			// yeniden bağlanınca catch-up taraması onu bulur.
		}
	})
}

// chatWatch, canlı trafik görülen sohbetleri hatırlar.
//
// Kopuk pencerede kalan mesajlar yalnız bu sohbetlerde aranabilir: hiç konuşma
// görülmemiş bir sohbetin geçmişini taramak gereksiz yere ağır.
type chatWatch struct {
	mu    sync.Mutex
	chats []string
}

// newChatWatch, boş bir izleme listesi döndürür.
func newChatWatch() *chatWatch {
	return &chatWatch{}
}

// remember, sohbeti listeye ekler, tekrar eklemez.
func (w *chatWatch) remember(jid string) {
	if jid == "" {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, known := range w.chats {
		if known == jid {
			return
		}
	}
	w.chats = append(w.chats, jid)
}

// known, listenin anlık kopyasını döndürür.
func (w *chatWatch) known() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]string(nil), w.chats...)
}

// parseArgs, komut satırını okur. --ad=değer ve --ad:değer ikisi de çalışır.
func parseArgs() args {
	raw := args{}

	fs := flag.NewFlagSet("tetikleyici", flag.ContinueOnError)

	var (
		bridge     = fs.String("bridge", "http://127.0.0.1:31415", "köprü taban adresi")
		configPath = fs.String("config", defaultConfig(), "kural config dosyası")
		seenFile   = fs.String("seen", defaultState("seen.txt"), "işlenmiş mesaj id'leri")
		logFile    = fs.String("log", defaultState("tetikleyici.log"), "günlük dosyası")
		window     = fs.Int("history", 5, "eyleme kaç mesaj bağlam verilsin (0 = yalnız tetikleyen)")
		dryRun     = fs.Bool("dry-run", false, "eylemleri çalıştırma, yalnız yaz")
		logOnly    = fs.Bool("log-only", false, "günlüğü yalnız dosyaya yaz")
		actionTO   = fs.Duration("action-timeout", defaultActionTimeout, "tek bir eylemin üst sınırı")
	)

	if err := fs.Parse(normalizeArgs(os.Args[1:])); err != nil {
		os.Exit(2)
	}

	if strings.TrimSpace(*configPath) == "" {
		fmt.Fprintln(os.Stderr, "hata: --config gerekli")
		os.Exit(2)
	}
	if *window < 0 {
		fmt.Fprintln(os.Stderr, "hata: --history negatif olamaz")
		os.Exit(2)
	}
	if *actionTO <= 0 {
		fmt.Fprintln(os.Stderr, "hata: --action-timeout sıfırdan büyük olmalı")
		os.Exit(2)
	}

	raw = args{
		configPath: *configPath,
		bridge:     strings.TrimRight(*bridge, "/"),
		seenFile:   *seenFile,
		logFile:    *logFile,
		window:     *window,
		dryRun:     *dryRun,
		logOnly:    *logOnly,
		actionTO:   *actionTO,
	}

	return raw
}

// waitFor, bağlam iptali ya da süre dolana dek bekler. İptalde false döner —
// çağıran döngüden çıkar. select'i her yerde tekrar etmemek için:
// bekleyişin iptal edilebilir olması, kapanışta 3. bir bekleyiş yaratmaz.
func waitFor(ctx context.Context, sure time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(sure):
		return true
	}
}

// normalizeArgs, --ad:değer yazımını --ad=değer biçimine çevirir; stdlib flag
// parser'ı yalnız ikincisini kabul eder.
func normalizeArgs(in []string) []string {
	out := make([]string, 0, len(in))

	for _, arg := range in {
		if strings.HasPrefix(arg, "--") {
			name, value, found := strings.Cut(strings.TrimPrefix(arg, "--"), ":")
			if found && name != "" {
				out = append(out, "--"+name+"="+value)
				continue
			}
		}
		out = append(out, arg)
	}

	return out
}

// defaultConfig, proje içindeki varsayılan config yoludur.
func defaultConfig() string {
	if path := os.Getenv("TETIKLEYICI_CONFIG"); path != "" {
		return path
	}
	return filepath.Join(projectDir(), "tetikleyici.yaml")
}

// defaultState, çalışma durumu dosyalarının bulunduğu dizini döndürür.
//
// Dosyalar proje içinde kalır; $HOME ve /tmp'ye dağılmaz.
func defaultState(name string) string {
	dir := os.Getenv("TETIKLEYICI_STATE_DIR")
	if dir == "" {
		dir = filepath.Join(projectDir(), ".tetikleyici")
	}
	return filepath.Join(dir, name)
}

// projectDir, binary'nin bulunduğu projenin kökünü bulur.
//
// Çalıştırma yerine bakmaz: ~/bin/tetikleyici çağrıldığında da doğru dizini
// bulabilmelidir. Yöntem: binary'nin yolundan yukarı çıkarken config dosyası
// aranır; bulunamazsa çalışma dizini kullanılır.
func projectDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe) // ~/bin
		if root := findProjectRoot(filepath.Dir(dir)); root != "" {
			return root
		}
	}

	if wd, err := os.Getwd(); err == nil {
		if root := findProjectRoot(wd); root != "" {
			return root
		}
	}

	return "."
}

// findProjectRoot, verilen dizinden yukarı çıkarken config dosyasını bulur.
//
// Bu, tetikleyiciyi ~/bin'den çalıştırdığında bile durum dosyalarının proje
// içine yazılmasını sağlar.
func findProjectRoot(start string) string {
	dir := start
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "tetikleyici.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
