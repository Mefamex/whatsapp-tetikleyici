// whatsapp-bridge-project/tetikleyici/action.go

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// payload, bir mesaj için hazırlanan ve eylemlere teslim edilen bağlamdır.
type payload struct {
	// Trigger, kuralı tutan mesajın kendisi.
	Trigger message

	// History, tetikleyen mesaj da dâhil son mesajlar. Sıralı ve yeniden
	// eskiye doğru.
	History []message

	// ConfigPath, komutun config'in nerede olduğunu öğrenebilmesi için.
	ConfigPath string
}

// env, komuta verilen ortam değişkenlerini üretir.
//
// Mesaj içeriği buraya düz metin olarak girer; komutun bunu ayrıştırması
// gerekirse JSON kanalını kullanması daha güvenli.
func (p payload) env() []string {
	env := os.Environ()

	set := func(key, value string) {
		env = append(env, key+"="+value)
	}

	set("TRIGGER_TEXT", p.Trigger.readableText())
	set("TRIGGER_NAME", p.Trigger.PushName)
	set("TRIGGER_JID", p.Trigger.From)
	set("TRIGGER_SENDER", p.Trigger.Sender)
	set("TRIGGER_ID", p.Trigger.ID)
	set("TRIGGER_TYPE", p.Trigger.Type)
	set("TRIGGER_TIME", formatClock(p.Trigger.Timestamp))
	set("TRIGGER_MESSAGES", formatWindow(p.History))
	set("TRIGGER_JSON", p.json())
	set("TRIGGER_CONFIG", p.ConfigPath)
	if p.Trigger.Quoted != nil {
		set("TRIGGER_QUOTED_TEXT", p.Trigger.Quoted.Text)
		set("TRIGGER_QUOTED_ID", p.Trigger.Quoted.ID)
		set("TRIGGER_QUOTED_SENDER", p.Trigger.Quoted.Sender)
		set("TRIGGER_QUOTED_TYPE", p.Trigger.Quoted.Type)
	}

	return env
}

// json, bağlamı makine tarafından okunabilir biçimde üretir.
func (p payload) json() string {
	var b strings.Builder
	b.WriteString("{\"trigger\":")
	b.WriteString(marshalJSON(p.Trigger))
	b.WriteString(",\"history\":[")
	for i, m := range p.History {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(marshalJSON(m))
	}
	b.WriteString("]}")
	return b.String()
}

// marshalJSON, bir mesajı JSON'a çevirir. Hata olursa null yazar — payload
// bozuk olsa bile komut çalışmaya devam etmelidir.
func marshalJSON(m message) string {
	raw, err := jsonMarshal(m)
	if err != nil {
		return "null"
	}
	return raw
}

// runAction, bir kuralın tek eylemini çalıştırır ve istenirse çıktısını
// tetikleyen sohbete WhatsApp mesajı olarak gönderir.
//
// Dönüş değeri gönderilen yanıtın metnidir (gönderilmediyse boş).
//
// Eylemler birbirinden bağımsızdır: biri başarısız olsa bile kuralın diğer
// eylemleri çalışır, çünkü kullanıcı onları ayrı ayrı yazmıştır.
//
// aiEtiEkle / debugCikti: eylem kendi kararını yazdıysa (add_aitag, do_debug)
// o karar geçerlidir; yazmadıysa config'teki global bayrak kullanılır.
// Neden: `ai_eti_ekle: false` global bayrağı tek yerde ya hep açık ya hep kapalı
// demekti; kişi bazlı kapatılamıyordu.
//
// debugCikti: true ise komutun ham stdout/stderr'ini (düşünce süreci dahil)
// WhatsApp'a gönderir; false = sadece son cevap (kırpılmış).
func runAction(ctx context.Context, opts options, act action, p payload, client *bridgeClient, aiEtiEkle bool, debugCikti bool) (string, error) {
	// Eylem kendi kararını yazdıysa global bayrağı geçersiz kıl.
	if act.AddAITag != nil {
		aiEtiEkle = *act.AddAITag
	}
	if act.DoDebug != nil {
		debugCikti = *act.DoDebug
	}

	var out string
	var err error

	// oturumIsı, bu eylem bir oturuma (session ID'li) bağlandı mı söyler.
	// Rate limit sonrası otomatik devam yalnız oturum eylemlerinde anlamlıdır:
	// "Continue, unstop" bir oturumun kaldığı yeri sürdürür.
	var oturumIsı bool

	switch act.Kind {
	case "command":
		out, err = runCommand(ctx, opts, act, p, debugCikti)
	case "session", "new_session":
		// Yalnız "session" ve dolu SessionID: sürdürülecek bir oturum var
		// demektir. new_session yeni oturum açar → devam anlamsızdır.
		oturumIsı = act.Kind == "session" && strings.TrimSpace(act.SessionID) != ""
		out, err = runOpencodeDirect(ctx, opts, act, p, araGonderici(ctx, opts, client, p, debugCikti))
	case "opencode":
		// Tek kişi tek kural: yön burada seçilir. Yol yoksa mesaj
		// SESSİZCE geçilir — "çıktı boş" bildirimi de gönderilmez, çünkü
		// kapalı hat "iş yapılmadı" demektir, "iş başarısız" değil.
		hat, varMi := act.yonlendir(p.Trigger)
		if !varMi {
			logf("  yön yok (%s tipi, izin kapalı) → sessizce geçildi", p.Trigger.Type)
			return "", nil
		}
		logf("  hat: %s", hat.ad)
		sendSeenIfAsked(ctx, client, act, p.Trigger)
		switch hat.tip {
		case "session":
			alt := act
			alt.Kind = "session"
			if mod, tSessionID := tunelDurumuAl(p.Trigger.From); mod == "session" && strings.TrimSpace(tSessionID) != "" {
				logf("  🎯 tünel session devrede: %s (kural varsayılanı: %s)", tSessionID, act.SessionID)
				alt.SessionID = tSessionID
			}
			oturumIsı = strings.TrimSpace(alt.SessionID) != ""
			out, err = runAgent(ctx, opts, alt, p, araGonderici(ctx, opts, client, p, debugCikti))
		case "command":
			alt := action{Kind: "command", Code: act.script(hat), Timeout: act.Timeout}
			out, err = runCommand(ctx, opts, alt, p, debugCikti)
		default:
			return "", fmt.Errorf("bilinmeyen hat tipi %q", hat.tip)
		}
	default:
		return "", fmt.Errorf("bilinmeyen eylem tipi %q", act.Kind)
	}

	if !act.Reply.On {
		return "", err
	}

	// ⏵ RATE LIMIT OTOMATİK DEVAM (sahip isteği 2026-10-06): model kota
	// hatası verdiğinde kullanıcıya hemen "biraz sonra tekrar dene" demek
	// yerine aynı oturuma "Continue, unstop" verilir ve işin tamamlanması
	// denenir. Başarılıysa cevap zaten gider; bu tur nil hata olarak döner
	// (bekleme başlatılmaz — iş bitti). Başarısızsa eski akış aynen işler:
	// kullanıcı tek mesaj alır, bekleme başlar.
	//
	// Yalnız bir oturuma bağlı (session ID'li) ve modelin ürettiği cevabı
	// gönderen eylemlerde anlamlıdır: devam, oturumun kaldığı yerden
	// sürmesini ister. Devam denemeleri SESSİZDİR (SessizHata) — kullanıcı
	// "kota doldu" ve "cevap geldi" diye iki çelişkili mesaj almaz.
	if err != nil {
		if kotali, _ := rateLimitMi(err.Error()); kotali && oturumIsı && !act.SessizHata &&
			act.Reply.Text == "" && strings.TrimSpace(act.SessionID) != "" {
			logf("  ⏵ rate limit → otomatik devam deneniyor (max %d, ara %s)",
				rateLimitDevamAdet, kisaSure(rateLimitDevamAra))
			if sonuc, dbErr := devamDene(ctx, opts, act, p, client, aiEtiEkle, debugCikti); dbErr == nil {
				logf("  ⏵ otomatik devam başarılı, cevap gönderildi (%d karakter)", len(sonuc))
				return sonuc, nil
			}
			logf("  ⏸ otomatik devam başarısız (%d deneme), eski akış sürüyor", rateLimitDevamAdet)
		}
	}

	// Sabit metin verilmişse çıktı yerine o gönderilir.
	reply := strings.TrimSpace(out)
	if act.Reply.Text != "" {
		reply = strings.TrimSpace(act.Reply.Text)
	}

	// Sessiz deneme (otomatik devam): bu tur size hiçbir şey göndermez.
	// Başarısız tur, kısmi çıktısı olsa bile sessizce döner; ana akış
	// (ya da bir sonraki devam turu) sonucu bildirir.
	if act.SessizHata && err != nil {
		return "", err
	}

	// Zaman aşımında öldürülen işin ürettiği kısmi cevap gönderilecek.
	// Ama kullanıcı mesajın KESİLDİĞİNİ bilmeli: yarım kalmış bir cevap
	// tamamlanmış gibi görünürse kullanıcı yanlış bilgilenir.
	//
	// Sabit metinli eylemlerde bu işaret EKLENMEZ: orada cevap zaten
	// kasıtlı olarak sabit, komutun çıktısı değil. "pong" gönderen bir
	// kural için "iş tamamlanmadı" demek yanlış olur.
	if reply != "" && act.Reply.Text == "" && errors.Is(err, context.DeadlineExceeded) {
		reply += "\n\n⏳ Zaman aşımına uğradı, iş tamamlanmadı."
		logf("  kısmi cevap gönderiliyor (zaman aşımı, iş tamamlanmadı)")
	}

	// AI etiketi ekle (config: ai_eti_ekle)
	if aiEtiEkle && reply != "" && act.Reply.Text == "" {
		reply += "\n\n🤖 AI asistanı"
	}
	if reply == "" {
		// Otomatik devam turu: başarısız tur sessizdir (hata olsun, çıktı boş
		// olsun). Ana akış tek mesajla bildirir — her tur "çıktı boş" dese
		// kullanıcı bir duvar yazısı seli alırdı.
		if act.SessizHata {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("yanıt istendi ama çıktı boş")
		}
		// Çıktı boş: ya eylem hata verdi ya da ajan hiçbir şey yazmadı.
		// Ölçüm (docs/KRITIK-SORUNLAR-2026-10-04.md #6): 124 tetiklemenin
		// 25'i (%20) bu yolda sessizce düştü. Kullanıcı "cevap vermiyor"
		// diye soruyordu, sebebi hiçbir yerde görünmüyordu.
		//
		// DÜZELTME: yanıt istenmişken hiçbir şey gönderilmezse sohbete kısa
		// bir sebep mesajı gider. Sessizlik, hatanın en pahalı biçimidir:
		// kullanıcı neyin yanlış gittiğini bilemez.
		sebep := bosCiktiSebebi(ctx, err)
		logf("  çıktı boş → kullanıcıya bildiriliyor: %s", sebep)

		if gonderErr := gonder(ctx, opts, client, p.Trigger.From, sebep); gonderErr != nil {
			return "", fmt.Errorf("%s (ayrıca bu bildirim de gönderilemedi: %v)", sebep, gonderErr)
		}
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("yanıt istendi ama çıktı boş (kullanıcıya bildirildi)")
	}

	// Hata olsa bile yanıt gönderilebilir: komut çıktıyı üretip sonradan
	// başarısız olmuş olabilir (örneğin sonda hatalı exit kodu).
	// Önce /reply numarası: ajan bir soru sorduysa bekleyen cevap numarası
	// atanır ve cevabın sonuna "> cevaplamak için /reply N" eklenir. Yalnız
	// ajanın ÜRETTİĞİ cevaplarda (sabit metin değil); sabit metinde soru
	// soran ajan olmadığından numara anlamsız olur.
	if act.Reply.Text == "" {
		reply, _ = soruNotu(opts.configPath, reply)
	}
	if sendErr := gonder(ctx, opts, client, p.Trigger.From, chatText(reply)); sendErr != nil {
		if err != nil {
			return "", fmt.Errorf("%w (ayrıca yanıt gönderilemedi: %v)", err, sendErr)
		}
		return "", fmt.Errorf("yanıt gönderilemedi: %w", sendErr)
	}

	return reply, nil
}

// ── Rate limit sonrası otomatik devam (2026-10-06) ─────────────────────
//
// Sahibin isteği: "rate limitte direkt continue diye mesaj gondersin".
// Model kota hatası verince kullanıcıya "biraz sonra tekrar dene" demek
// yetmiyor — iş YARIM kalıyor ve kullanıcıya kalmış oluyor. Otomatik devam,
// aynı oturuma birkaç kez devam imleci verir; biri tutarsa cevap
// kendiliğinden gider.

// devamPrompt, oturumu sürdüren imleç. opencode aynı --session ile
// çağrıldığında bu metin oturumun devam mesajı olur. Sahibin elle kullandığı
// kalıp: "Continue, unstop" + çaba modu satırı.
const devamPrompt = "Continue, unstop\n\n[effort mode: ultra high]"

// rateLimitDevamAdet, kota hatası sonrası denenecek devam sayısı.
// Sıfır art arda devam etmek anlamsız: kota birkaç dakika içinde
// toparlanmaz, her deneme kotayı biraz daha yakar. İki deneme hem "anlık
// sıçrama" hem "yarım dakika sonra toparlanma" durumunu kapsar.
const rateLimitDevamAdet = 2

// rateLimitDevamAra, devam denemeleri arasındaki nefes. Sıfır yapmak,
// kota toparlanmadan üst üste deneme demekti ve her deneme yeni kotalı
// çağrı = kotayı daha da bitirmek.
const rateLimitDevamAra = 15 * time.Second

// devamDene, oturumu devam imleciyle sürdürmeyi dener.
//
// Başarı: bir denemede cevap üretilirse o cevap ZATEN sohbete gönderilmiş
// olur (runAction içindeki gonder yolu); metni de çağırana döner ve dış
// akış "yanıt gönderildi"yi görür.
//
// Hata: hiçbir deneme tutmazsa çağıran bilgilendirilir. Denemeler
// SESSİZDİR (SessizHata): kullanıcıya yalnız ana akışta tek mesaj gider —
// "kota doldu" ve "cevap geldi" diye çelişkili çift mesaj değil.
//
// Rate limit DIŞI bir hata görülürse devam anlamsızdır, orada bırakılır:
// devam imleci kotayı değil, oturumun yarım kalan işini geri ister.
func devamDene(ctx context.Context, opts options, act action, p payload, client *bridgeClient, aiEtiEkle bool, debugCikti bool) (string, error) {
	var sonHata error
	for i := 1; i <= rateLimitDevamAdet; i++ {
		if i > 1 {
			ara := opts.devamAra
			if ara > 0 {
				timer := time.NewTimer(ara)
				select {
				case <-ctx.Done():
					timer.Stop()
					return "", ctx.Err()
				case <-timer.C:
				}
			}
		}

		alt := act
		alt.Kind = "session"
		alt.DevamMetni = devamPrompt
		alt.SessizHata = true
		alt.Reply.Text = "" // sabit metin, devamda anlamsız (yukarıda elendi; garanti)

		sonuc, err := runAction(ctx, opts, alt, p, client, aiEtiEkle, debugCikti)
		if err == nil && sonuc != "" {
			return sonuc, nil
		}
		if err == nil {
			err = fmt.Errorf("devam boş çıktı")
		}
		sonHata = err
		if kotali, _ := rateLimitMi(err.Error()); !kotali {
			return "", err
		}
	}
	return "", sonHata
}

// gonder, sohbete tek mesaj yollar.
//
// Gönderim süresi ayrı bir bağlamla sınırlıdır: eylemin kendi zaman aşımı
// dolmuş olsa bile bildirim gitmelidir. Bağlam iptal edilmişse (eylem
// zaman aşımı) bağlam olmadan denenır — süreç kapanışı yine de iptal eder.
func gonder(ctx context.Context, opts options, client *bridgeClient, jid, metin string) error {
	if client == nil {
		// Köprü yoksa gönderilemez. Bu bir hata değil, KÖPRÜSÜZ kip:
		// testler ve "ajanı çalıştır, cevabı sen al" kullanımı. Panik yerine
		// açık hata — çağıran zaten gönderemez, yanlışlıkla "gönderildi"
		// sanmasın.
		return fmt.Errorf("köprü yok, gönderilemedi")
	}

	// Bastırma kontrolü kendi kısa zaman aşımıyla çalışır: gönderim
	// zaman aşımı (opts.sendTimeout) uzun olabilir, geçmiş sorgusu
	// köprüye bağlı ve 20 saniyeye kadar sürebiliyor.
	if aynen, sebep := zatenGonderilmis(ctx, client, jid, metin); aynen {
		logf("  ⏭ çift gönderim bastırıldı (%s): %s", sebep, truncate(metin, 60))
		return nil
	}

	giden := ctx
	if ctx.Err() != nil {
		giden = context.WithoutCancel(ctx)
	}

	sendCtx, cancel := context.WithTimeout(giden, opts.sendTimeout)
	defer cancel()

	return client.sendText(sendCtx, jid, metin)
}

// zamanBastirmaPencere, aynı metnin "yeniden gönderilmiş" sayılacağı
// süre. Kısa tutulur: kullanıcı aynı cevabı bilerek iki kez isteyebilir
// ("tekrar söyle"), o durumda bastırmak YANLIŞ olur ve kullanıcı cevabı
// hiç almaz.
const zamanBastirmaPencere = 2 * time.Minute

// zamanDamgasiBelirsizligi, köprü zaman damgalarının çözünürlüğüdür.
//
// Köprü Unix saniyesi gönderir; mesajın saniye içindeki konumu bilinmez.
// Bu yüzden 119,5 saniye önce giden mesaj "120,5 saniye önce" hesaplanabilir
// ve sınırın hemen dışında görünür. Tolerans olmazsa pencere sınırındaki
// mesajlar rastgele kaçar — yani çift gönderim koruması zamanla zayıflar.
//
// Yön seçimi: belirsizlik pencerenin DIŞINA doğru açılır, içe değil.
// Bir mesajın yaşı belirsizken bastırmak, kullanıcının cevabı yutulması
// demektir; kaçırmak ise en kötü hâlde bir çift mesaj.
const zamanDamgasiBelirsizligi = time.Second

// zatenGonderilmis, bu metin az önce gidildi mi diye bakar.
//
// Dönüş: (bastırılsın mı, gerekçe).
//
// Geçmiş ALINAMAZSA bastırma YAPILMAZ, yani gönderim yapılır. Yanlış
// bastırma, kullanıcının cevabı hiç almamasından çok daha kötüdür: en
// azından yanlış giden mesajı kullanıcı görebilir, hiç mesaj
// gelmemesini açıklayamaz. Köprü yeniden bağlandığında geçmiş boş
// döner; bu normal bir durum, hata sayılmaz.
func zatenGonderilmis(ctx context.Context, client *bridgeClient, jid, metin string) (bool, string) {
	if strings.TrimSpace(metin) == "" {
		return false, "boş metin"
	}
	if client == nil {
		// Köprü yoksa çift gönderim denetimi yapılamaz. Testler ve
		// "yalnız ajan çalıştır" kipı buraya düşer. Sessiz geçmek doğru:
		// bilinmeyen bir hatada kullanıcıya "gönderilemedi" demek, mesajı
		// hiç göndermemekten kötüdür.
		return false, "köprü yok"
	}

	// Bakış kısa tutulur: kullanıcı mesajı bekliyor. Bakılamazsa gönderilir.
	bakisCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()

	msgs, err := client.historyHizli(bakisCtx, jid, 12)
	if err != nil {
		return false, "geçmiş alınamadı: " + err.Error()
	}

	simdi := time.Now()
	hedef := teklastirMetin(metin)

	for _, m := range msgs {
		if !m.fromMe() {
			continue
		}
		// Zaman damgası köprüde saniye cinsinden; sıfır ya da hatalıysa güvenme.
		if m.Timestamp <= 0 {
			continue
		}
		yas := simdi.Sub(time.Unix(m.Timestamp, 0))
		if yas < 0 {
			// Saat kayması olabilir (köprü UTC, sunucu yerel). Yine de
			// "gelecekte giden" mesaj pencerede sayılabilir: yanlış
			// bastırmaktansa bastırmamak yeğdir.
			yas = 0
		}
		if yas > zamanBastirmaPencere+zamanDamgasiBelirsizligi {
			continue
		}
		if teklastirMetin(m.Text) == hedef {
			return true, fmt.Sprintf("%.0f sn önce aynı metin gitti", yas.Seconds())
		}
	}
	return false, ""
}

// teklastirMetin, iki metnin "aynı" sayılıp sayılmayacağına karar verir.
//
// Üç katman: kenar boşluğu, biçim kabuğu (``` ** __ `), satır sonu ve
// tekrarlı boşluk. Model aynı cevabı "**Toplam 23.050 TL**" ve
// "Toplam 23.050 TL" biçimlerinde verebilir; bunlar aynı cevaptır.
func teklastirMetin(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	for _, isaret := range []string{"```", "**", "__", "`"} {
		s = strings.ReplaceAll(s, isaret, "")
	}

	// Satır sonlarını ve tekrarlı boşlukları tek boşluğa indir.
	var b strings.Builder
	b.Grow(len(s))
	oncekiBosluk := false
	for _, r := range s {
		switch r {
		case '\n', '\r', '\t', ' ':
			oncekiBosluk = true
			continue
		}
		if oncekiBosluk && b.Len() > 0 {
			b.WriteByte(' ')
		}
		oncekiBosluk = false
		b.WriteRune(r)
	}

	// Büyük/küçük harf: WhatsApp'ta aynı cevabın farklı yazımı kullanıcıya
	// çift gönderim gibi görünmez ama sayımı bozar.
	return strings.ToLower(strings.TrimSpace(b.String()))
}

// bosCiktiSebebi, çıktı boş çıktığında kullanıcıya ne söyleneceğini seçer.
//
// Kullanıcı modelin jargonunu değil, ne olup bittiğini bilmeli. Hata metni
// dosya yolu ya da session kimliği sızdırabilir; bu yüzden kullanıcıya kısa
// bir açıklama gider, ayrıntı yalnız günlüğe yazılır.
func bosCiktiSebebi(ctx context.Context, err error) string {
	metin := ""
	if err != nil {
		metin = err.Error()
	}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "İş zaman aşımına uğradı, cevap üretilemedi. Daha kısa bir istek yazabilirsin."

	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		return "Tetikleyici yeniden başlatıldı, istek yarıda kaldı."

	case strings.Contains(metin, "signal: killed"):
		return "İş zaman aşımında durduruldu, cevap üretilemedi. Daha kısa bir istek yazabilirsin."

	case strings.Contains(metin, "Rate limit"), strings.Contains(metin, "rate limit"):
		return "Model şu an yük altında (rate limit). Biraz sonra tekrar dene."

	case strings.Contains(metin, "Model unavailable"), strings.Contains(metin, "provider.no-route"), strings.Contains(metin, "no-route"):
		return "Model sağlayıcı tarafında geçici olarak erişilemez durumda. Model ayarı kontrol edilmelidir."

	case strings.Contains(metin, "context canceled"):
		return "Bağlantı koptu, istek tamamlanamadı. Tekrar yazabilirsin."

	case err != nil:
		return "İş tamamlanamadı, cevap üretilemedi. Ayrıntı günlüğe yazıldı."
	}

	return "Cevap üretilmedi (çıktı boş). Farklı şekilde sormayı dene."
}

// userShell, komutların çalışacağı kabuğu seçer.
//
// /cmd "kendi bilgisayarının terminali" demek: kullanıcının giriş kabuğu (zsh)
// kullanılır, çünkü zsh'ye özgü sözdizimi (extendedglob, glob nitelikleri)
// sh'da çalışmaz ve komut sessizce bozulur. Sıra: $SHELL → zsh → sh.
func userShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	if _, err := os.Stat("/usr/bin/zsh"); err == nil {
		return "/usr/bin/zsh"
	}
	return "sh"
}

// runCommand, kabuk komutunu çalıştırır ve stdout'unu döndürür.
//
// Mesaj üç ayrı kanaldan geçer: stdin (ham metin), ortam değişkenleri ve
// {mesajlar} yer tutucusu. Yer tutucu kullanılıyorsa kabuk kaçışı uygulanır.
//
// debugCikti: true ise komutun ham stdout/stderr'ini WhatsApp'a gönderir.
func runCommand(ctx context.Context, opts options, act action, p payload, debugCikti bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, act.sure(opts))
	defer cancel()

	script := expandPlaceholder(act.Code, formatWindow(p.History))

	cmd := exec.CommandContext(ctx, userShell(), "-c", script)
	cmd.Stdin = strings.NewReader(p.Trigger.readableText())
	env := p.env()
	if debugCikti {
		env = append(env, "DEBUG_CIKTI=true")
	}
	// Zamanlanmış mesaj: komut tarafı (/zamanla → cmd.py) bayrağı ve iş
	// dizinini env'den alır. Kapalıyken komut reddedilir (zamanlanmis.go).
	env = append(env, zamanlanmisEnv(opts)...)
	cmd.Env = env
	// Çıktı yakalanır: yanıt isteniyorsa oradan gönderilir. Terminale de
	// yazılır, yoksa uzun çalışan komutlarda hiçbir iz kalmazdı.
	//
	// stderr de aynı anda İKİ yere gider: ekrana (eskisi gibi) ve son
	// baytlarını tutan tampona. Tamponun adresi hata_ciktisi.go'daki
	// açıklamada: yalnız ekrana yazmak stderr'i hatadan KESİYORDU, bu da
	// rate limit tanımasını ve kullanıcı bildirimini öldürüyordu.
	var buf bytes.Buffer
	komutHatasi := &hataCiktisi{}
	cmd.Stdout = &buf
	cmd.Stderr = komutHatasi.cikti()

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("başlatılamadı: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		out := buf.String()
		if out != "" {
			logf("    komut çıktısı: %s", truncate(out, 300))
		}
		if err != nil {
			if ek := komutHatasi.sonek(); ek != "" {
				logf("    komut hata çıktısı:%s", ek)
			}
			return out, cikisHatasi(fmt.Errorf("çıkış hatası: %w", err), komutHatasi)
		}
		return out, nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		// Zaman aşımı, bağlam hatası olarak SARILIR. Sarılmazsa
		// errors.Is(err, context.DeadlineExceeded) çalışmaz ve
		// bosCiktiSebebi kullanıcıya "iş tamamlanamadı" gibi belirsiz
		// bir mesaj yollar. Test bu yolu zorlar.
		return buf.String(), fmt.Errorf("%w: %s sonunda zaman aşımı",
			context.DeadlineExceeded, act.sure(opts))
	}
}

// araGonderici, ajanın ara mesajlarını sohbete yollayan geri çağırma yapar.
//
// Yalnız do_debug açıkken çağrılır. Düşünce blokları ve tool çıktıları YOKTUR:
// opencode JSON akışında `tool_use` olayı olarak gelir ve burada elenir. Kullanıcı
// "bir saniye" gibi ilerleme mesajlarını görür, terminal kirliliğini görmez.
func araGonderici(ctx context.Context, opts options, client *bridgeClient, p payload, debugCikti bool) func(string) {
	if !debugCikti || client == nil {
		return nil
	}
	return func(metin string) {
		metin = strings.TrimSpace(metin)
		if metin == "" {
			return
		}
		if err := gonder(ctx, opts, client, p.Trigger.From, chatText(metin)); err != nil {
			logf("  ara mesaj gönderilemedi: %v", err)
			return
		}
		logf("  ara mesaj gönderildi (%d karakter): %s", len(metin), truncate(metin, 120))
	}
}

// sendSeenIfAsked, eylem send_seen: true yazdıysa mesajı okundu işaretler.
//
// Kural yazmamışsa hiçbir şey yapılmaz: global `okundu_isaretle` motor
// seviyesinde zaten uygulanıyor, burada ikinci kez çağırmak mavi tik'i iki kez
// yollardı.
func sendSeenIfAsked(ctx context.Context, client *bridgeClient, act action, m message) {
	if act.SendSeen == nil || !*act.SendSeen || client == nil {
		return
	}
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := client.markRead(markCtx, m.From, []string{m.ID}); err != nil {
		logf("  okundu işaretleme hatası: %v", err)
		return
	}
	logf("  okundu işaretlendi (mavi tik)")
}

// runOpencode, mesajı bir opencode oturumuna verir ve ajanın cevabını döndürür.
//
// opencode her zaman `--format json` ile çağrılır. Ham stdout'un sorunu şuydu:
// ajan hem "bir saniye" gibi ara mesajlar hem de tool çıktıları düz metin
// olarak dökülüyor, kullanıcı da tek mesajda ikisini birden alıyordu. JSON
// akışında metin (`text`) ile tool (`tool_use`) AYRI olay olduğundan yalnız
// metinler seçilebiliyor.
//
// araMesaj verilirse (do_debug: true) metin olaylarının sonuncusu hariç her
// biri geri çağrım üzerinden sohbete gider: ara mesajlar görünür, tool çıktısı
// görünmez.
// aktifMotorAl, kullanıcının seçtiği aktif ajan motorunu döndürür.
// .tetikleyici/aktif_motor.txt dosyasında tutulur (agy, claude, opencode).
// Varsayılan: agy.
func aktifMotorAl() string {
	yol := defaultState("aktif_motor.txt")
	icerik, err := os.ReadFile(yol)
	if err == nil {
		m := strings.TrimSpace(strings.ToLower(string(icerik)))
		if m == "claude" || m == "opencode" || m == "agy" {
			return m
		}
	}
	return "agy"
}

// runAgent, seçili motoru (agy, claude, opencode) çalıştırır.
func runAgent(ctx context.Context, opts options, act action, p payload, araMesaj func(string)) (string, error) {
	// Test ortamında veya özel yolda opts.opencode geçilmişse doğrudan onu kullan.
	if opts.opencode != "" &&
		opts.opencode != "~/.opencode/bin/opencode" &&
		!strings.HasSuffix(opts.opencode, "/.opencode/bin/opencode") {
		return runOpencodeDirect(ctx, opts, act, p, araMesaj)
	}

	motor := aktifMotorAl()
	logf("  aktif motor: %s", motor)

	if motor == "opencode" {
		return runOpencodeDirect(ctx, opts, act, p, araMesaj)
	}

	prompt := buildPrompt(p)
	if t := strings.TrimSpace(act.DevamMetni); t != "" {
		prompt = t
	}

	var bin string
	var args []string

	switch motor {
	case "agy":
		bin = expandHome("~/.local/bin/agy")
		if !filepathIsAbs(bin) {
			if resolved, err := exec.LookPath(bin); err == nil {
				bin = resolved
			}
		}
		if _, err := os.Stat(bin); err != nil {
			if resolved, err := exec.LookPath("agy"); err == nil {
				bin = resolved
			} else {
				return "", fmt.Errorf("agy bulunamadı: %s", bin)
			}
		}
		args = []string{"-p", prompt, "--dangerously-skip-permissions"}

	case "claude":
		bin = expandHome("~/.local/bin/claude")
		if !filepathIsAbs(bin) {
			if resolved, err := exec.LookPath(bin); err == nil {
				bin = resolved
			}
		}
		if _, err := os.Stat(bin); err != nil {
			if resolved, err := exec.LookPath("claude"); err == nil {
				bin = resolved
			} else {
				return "", fmt.Errorf("claude bulunamadı: %s", bin)
			}
		}
		args = []string{"-p", prompt, "--dangerously-skip-permissions"}
	}

	serbestBirak, err := oturumKilidiAl(ctx, act.SessionID)
	if err != nil {
		return "", fmt.Errorf("oturum kilidi alınamadı (%s): %w", act.SessionID, err)
	}
	defer serbestBirak()

	ctx, cancel := context.WithTimeout(ctx, act.sure(opts))
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = projectDir()
	cmd.Env = p.env()
	var buf bytes.Buffer
	ajanHatasi := &hataCiktisi{}
	cmd.Stdout = &buf
	cmd.Stderr = ajanHatasi.cikti()

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("başlatılamadı: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		metin := sonCevap(buf.String(), araMesaj)
		if err != nil {
			if ek := ajanHatasi.sonek(); ek != "" {
				logf("  ajan hata çıktısı:%s", truncate(ek, 300))
			}
			return metin, cikisHatasi(fmt.Errorf("çıkış hatası: %w", err), ajanHatasi)
		}
		return metin, nil

	case <-ctx.Done():
		_ = cmd.Process.Kill()

		if kismi := strings.TrimSpace(sonCevap(buf.String(), araMesaj)); kismi != "" {
			logf("  zaman aşımı: %d karakterlik kısmi çıktı kurtarıldı", len(kismi))
			return kismi, fmt.Errorf("%w: %s sonunda zaman aşımı (kısmi cevap gönderildi)",
				context.DeadlineExceeded, act.sure(opts))
		}
		return "", fmt.Errorf("%w: %s sonunda zaman aşımı, ajan hiçbir şey yazmadı",
			context.DeadlineExceeded, act.sure(opts))
	}
}

// runOpencodeDirect, doğrudan opencode binary'sini çalıştırır.
func runOpencodeDirect(ctx context.Context, opts options, act action, p payload, araMesaj func(string)) (string, error) {
	bin := expandHome(opts.opencode)
	if bin == "" {
		return "", fmt.Errorf("config'de \"opencode\" yolu tanımlı değil")
	}

	if !filepathIsAbs(bin) {
		if resolved, err := exec.LookPath(bin); err == nil {
			bin = resolved
		}
	}
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("opencode bulunamadı: %s", bin)
	}

	serbestBirak, err := oturumKilidiAl(ctx, act.SessionID)
	if err != nil {
		return "", fmt.Errorf("oturum kilidi alınamadı (%s): %w", act.SessionID, err)
	}
	defer serbestBirak()

	ctx, cancel := context.WithTimeout(ctx, act.sure(opts))
	defer cancel()

	prompt := buildPrompt(p)
	if t := strings.TrimSpace(act.DevamMetni); t != "" {
		prompt = t
	}
	args := append([]string{"run"}, opencodeArgs(act, prompt)...)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = projectDir()
	cmd.Env = p.env()
	var buf bytes.Buffer
	ajanHatasi := &hataCiktisi{}
	cmd.Stdout = &buf
	cmd.Stderr = ajanHatasi.cikti()

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("başlatılamadı: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		metin := sonCevap(buf.String(), araMesaj)
		if err != nil {
			if ek := ajanHatasi.sonek(); ek != "" {
				logf("  ajan hata çıktısı:%s", truncate(ek, 300))
			}
			return metin, cikisHatasi(fmt.Errorf("çıkış hatası: %w", err), ajanHatasi)
		}
		return metin, nil

	case <-ctx.Done():
		_ = cmd.Process.Kill()

		if kismi := strings.TrimSpace(sonCevap(buf.String(), araMesaj)); kismi != "" {
			logf("  zaman aşımı: %d karakterlik kısmi çıktı kurtarıldı", len(kismi))
			return kismi, fmt.Errorf("%w: %s sonunda zaman aşımı (kısmi cevap gönderildi)",
				context.DeadlineExceeded, act.sure(opts))
		}
		return "", fmt.Errorf("%w: %s sonunda zaman aşımı, ajan hiçbir şey yazmadı",
			context.DeadlineExceeded, act.sure(opts))
	}
}

// runOpencode, geriye dönük uyumluluk için runAgent'ı çağırır.
func runOpencode(ctx context.Context, opts options, act action, p payload, araMesaj func(string)) (string, error) {
	return runAgent(ctx, opts, act, p, araMesaj)
}

// opencodeOlay, `opencode run --format json` akışının tek satırı.
//
// Yalnız gereken alanlar çözülür: olayın adı ve parçanın türü/metni. Tool
// olaylarının `state.output` alanı bilinçli olarak ÇÖZÜLMEZ — okunmamalı.
type opencodeOlay struct {
	Type string `json:"type"`
	Part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"part"`
}

// metinParcalari, ham stdout'tan ajanın yazdığı metinleri sırayla çıkarır.
//
// Düşünme blokları ve tool çıktıları elenir. Çözülemeyen satırlar yok sayılır:
// opencode sürümleri yeni olay tipleri ekleyebiliyor, bunlar akışı bozmamalı.
func metinParcalari(ham string) []string {
	var parcalar []string
	for _, satir := range strings.Split(ham, "\n") {
		satir = strings.TrimSpace(satir)
		if satir == "" || !strings.HasPrefix(satir, "{") {
			continue
		}
		var olay opencodeOlay
		if err := json.Unmarshal([]byte(satir), &olay); err != nil {
			continue
		}
		if olay.Type != "text" || olay.Part.Type != "text" {
			continue
		}
		if metin := strings.TrimSpace(olay.Part.Text); metin != "" {
			parcalar = append(parcalar, metin)
		}
	}
	return parcalar
}

// sonCevap, akıştan nihai cevabı seçer.
//
// Doğrulandı (2026-10-06, `opencode run --format json`): olaylar
// step_start → tool_use → step_finish → step_start → text düzeninde gelir.
// Metin parçaları sırayla toplanır ve EN SONUNCU nihai cevaptır; öncekiler
// ajanın ara mesajlarıdır ("bir saniye", "dosyayı okuyorum").
//
// JSON çözülemiyorsa ham çıktı aynen döner: eski opencode sürümü ya da farklı
// bir çıktı biçimi sessizce "boş cevap" vermemelidir.
func sonCevap(ham string, araMesaj func(string)) string {
	parcalar := metinParcalari(ham)
	if len(parcalar) == 0 {
		kirpilmis := strings.TrimSpace(ham)
		// Ham çıktı bir JSON hata nesnesi ise (örn. {"type":"error",...}),
		// bu bir sohbet cevabı değildir; kullanıcıya ham hata JSON'ı gönderilmez.
		if strings.HasPrefix(kirpilmis, "{\"type\":\"error\"") || strings.Contains(kirpilmis, `"error":{`) {
			return ""
		}
		return kirpilmis
	}

	if araMesaj != nil {
		for _, ara := range parcalar[:len(parcalar)-1] {
			araMesaj(ara)
		}
	} else if len(parcalar) > 1 {
		logf("  ajan %d ara mesaj üretti, yalnız son cevap gönderiliyor (do_debug: true → hepsi)",
			len(parcalar)-1)
	}
	return parcalar[len(parcalar)-1]
}

// opencodeArgs, "opencode run" için argümanları kurar.
//
// session --session ile oturumu sabitler; cevabın yanlış konuşmaya düşmemesi
// için gereklidir. yeni_session ise bilerek ne --session ne --continue
// kullanır: amaç her tetiklemede taze konuşmadır, --continue ise "son çalışan
// oturum"a bağlanır ve bu başka bir sohbetin oturumu olabilir.
func opencodeArgs(act action, prompt string) []string {
	var args []string

	// --agent, izin kısıtlarını taşır. YANIT VEREN HER AKIŞTA verilmelidir:
	// ajan `whatsapp_send` çağırırsa cevap iki kez gider.
	if ad := strings.TrimSpace(act.Agent); ad != "" {
		args = append(args, "--agent", ad)
	}

	// --format json olmadan stdout, ajanın metniyle tool çıktısının
	// karışımıdır. Prompt'tan ÖNCE gelmesi şart: opencode mesajı sondan okur.
	args = append(args, "--format", "json")

	if act.Kind == "session" && strings.TrimSpace(act.SessionID) != "" {
		args = append(args, "--session", act.SessionID)
	}

	return append(args, prompt)
}

// buildPrompt, ajana verilecek metni kurar.
func buildPrompt(p payload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WhatsApp'tan %s yazdı (JID: %s).\n\n", p.Trigger.label(), p.Trigger.From)
	if p.Trigger.Quoted != nil && strings.TrimSpace(p.Trigger.Quoted.Text) != "" {
		fmt.Fprintf(&b, "[Alıntılanan/Yanıtlanan Mesaj]: %q\n\n", p.Trigger.Quoted.Text)
	}
	b.WriteString("Son mesajlar:\n")
	b.WriteString(formatWindow(p.History))
	b.WriteString("\n\nNot: Yanıtını doğrudan metin olarak üret; çıktın doğrudan WhatsApp üzerinden sahibe iletilecektir.")
	return b.String()
}

// expandPlaceholder, komut içindeki {mesajlar} yer tutucusunu geçmiş metniyle
// değiştirir. Mesaj içeriği tek tırnak içine alınır, böylece yazılan kabuk
// komutunda özel karakterler kırılmaz.
func expandPlaceholder(script, context string) string {
	if !strings.Contains(script, "{mesajlar}") {
		return script
	}
	return strings.ReplaceAll(script, "{mesajlar}", shellQuote(context))
}

// shellQuote, metni kabuk için güvenli tek argümana çevirir.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// filepathIsAbs, yolun mutlak olup olmadığını söyler.
func filepathIsAbs(path string) bool {
	return strings.HasPrefix(path, "/")
}

// defaultActionTimeout, tek bir eylemin üst sınırı.
const defaultActionTimeout = 3 * time.Minute
