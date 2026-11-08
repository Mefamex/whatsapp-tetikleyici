package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ruleSet, config dosyasının tamamıdır. Mesaj başına yeniden okunur.
type ruleSet struct {
	// OpenCode, session ve new_session eylemlerinin çağıracağı binary.
	// Config'de tutulur ki araç hiçbir makine yolunu içine gömmesin.
	OpenCode string `yaml:"opencode"`

	// OkunduIsaretle, gelen her mesajı okundu işaretler (mavi tik).
	// Varsayılan: false. true = whatsapp_mark_read çağrılır.
	OkunduIsaretle bool `yaml:"okundu_isaretle"`

	// AIEtiEkle, her cevabın sonuna "🤖 AI asistanı" ekler.
	// Varsayılan: false. true = cevap metninin sonuna otomatik eklenir.
	AIEtiEkle bool `yaml:"ai_eti_ekle"`

	// DebugCikti, opencode'nin ham stdout/stderr'ini (düşünce süreci,
	// ara mesajlar dahil) doğrudan WhatsApp'a gönderir.
	// Varsayılan: false. true = debug için ham çıktı; false = sadece son cevap.
	DebugCikti bool `yaml:"debug_cikti"`

	// KameraIzni, bilgisayar kamerasından tek kare çekip mefamex'e
	// göndermeye izin verir. Varsayılan: false (KAPALI — mahremiyet).
	// true = `/camera` komutu çalışır.
	KameraIzni bool `yaml:"kamera_izni"`

	// CatchUpMaxAge, açılış veya bağlantı yenileme kurtarma taramasında (catch-up)
	// bu süreden eski mesajların atlanmasını ve spam yanıt verilmemesini sağlar.
	// Örnek: "10m", "15m", "1h". Boşsa veya geçersizse varsayılan: 10m.
	CatchUpMaxAge string `yaml:"catchup_max_age"`

	// Users, kullanıcı başına yetki ayarları (3 aşamalı model).
	// 1. Kişi config'de var mı? (users listesinde)
	// 2. / ile gelen mesajlara yetkisi var mı? (allow_slash)
	// 3. /cmd özel yetkisi var mı? (allow_cmd)
	// Heartbeat, "admin konuşmuyor mu" hatırlatmasıdır. Kural DEĞİLDİR:
	// mesaj eşleşmesiyle ilgisi yoktur, tetikleyici kendi işidir.
	//
	// Boş bırakılırsa (SessionID boş) hiç çalışmaz.
	Heartbeat heartbeat `yaml:"heartbeat"`

	// Notify, tetikleyicinin KENDİ yaşam döngüsü bildirimleridir. Kural
	// Değildir: mesaj eşleşmesiyle ilgisi yoktur; tetikleyici açılıp
	// kapanırken ve köprü bağlantısı değişirken yalnız `to`'daki kimliğe
	// haber verir.
	//
	// Boş bırakılırsa ya da hiçbir on_* bayrağı açık değilse hiçbir şey
	// gönderilmez.
	Notify notifyConfig `yaml:"notify"`

	// Zamanlanmis, "şu zaman sonra şu mesajı gönder" özelliğidir
	// (`/zamanla <süre> <mesaj>`). Kural değildir: gönderimler tetikleyicinin
	// kendi sürecinde gerçekleşir. Yazılmazsa veya acik: false ise KAPALIDIR
	// (MEVZU: kod varsayılanı false — açık kapı yok).
	Zamanlanmis zamanlanmisConfig `yaml:"zamanlanmis_mesaj"`

	// Tehlikeli, TANINMAYAN numaralardan gelen şüpheli mesajları (link
	// içeriyor VEYA dosya/medya eki taşıyor) admin'e bildirir. Kural
	// değildir ve YANIT DEĞİLDİR: gelen numaraya hiçbir şey gönderilmez,
	// yalnız `to`'daki kimliğe kısa bir uyarı gider. Yazılmazsa veya
	// acik: false ise KAPALIDIR.
	Tehlikeli tehlikeliConfig `yaml:"tehlikeli_mesaj"`

	// Users, kullanıcı başına yetki ayarları (3 aşamalı model).
	// 1. Kişi config'de var mı? (users listesinde)
	// 2. / ile gelen mesajlara yetkisi var mı? (allow_slash)
	// 3. /cmd özel yetkisi var mı? (allow_cmd)
	Users usersConfig `yaml:"users"`

	// Rules sırayla değerlendirilir; hepsi de tutarsa hepsi çalışır.
	Rules []rule `yaml:"rules"`
}

// zamanlanmisConfig, zamanlanmış mesaj ayarları.
type zamanlanmisConfig struct {
	// Acik, özelliği açar. Yazılmazsa nil kalır → kapalı. `true` yazılınca
	// /zamanla komutu çalışır ve tetikleyici zamanlanan gönderimleri yapar.
	Acik *bool `yaml:"acik"`
}

// enabled, zamanlanmış mesaj özelliği açık mı.
func (z zamanlanmisConfig) enabled() bool {
	return z.Acik != nil && *z.Acik
}

// tehlikeliConfig, şüpheli mesaj uyarısı ayarları.
type tehlikeliConfig struct {
	// Acik, özelliği açar. Yazılmazsa nil kalır → kapalı.
	Acik *bool `yaml:"acik"`

	// To, uyarıların gideceği kimlik (ör. mefamex LID'i). Boşsa özellik
	// kapalıdır — kime gideceği belli olmayan uyarı gönderilmez.
	To string `yaml:"to"`

	// Aralik, aynı numaradan uyarılar arası asgari süre. Yazılmazsa
	// varsayılan 1 saat (spam koruması).
	Aralik string `yaml:"aralik"`
}

// enabled, şüpheli uyarı özelliği açık ve hedef tanımlı mı.
func (t tehlikeliConfig) enabled() bool {
	return t.Acik != nil && *t.Acik && strings.TrimSpace(t.To) != ""
}

// userConfig, kullanıcı başına yetki ayarları.
type userConfig struct {
	// Numbers, kullanıcının telefon/LID numaraları (ör. 905551234567, 12345678901234@lid)
	Numbers stringList `yaml:"number"`

	// Names, kullanıcının push_name'leri (ör. Mefamex)
	Names stringList `yaml:"name"`

	// AllowSlash, `/` ile başlayan mesajlara yetki (prefix: "/" kuralı için)
	AllowSlash bool `yaml:"allow_slash"`

	// AllowCmd, /cmd özel yetkisi
	AllowCmd bool `yaml:"allow_cmd"`

	// AllowCamera, /camera yetkisi
	AllowCamera bool `yaml:"allow_camera"`

	// AllowDurum, /durum yetkisi
	AllowDurum bool `yaml:"allow_durum"`

	// AllowZamanla, /zamanla yetkisi
	AllowZamanla bool `yaml:"allow_zamanla"`

	// AllowReply, /reply yetkisi
	AllowReply bool `yaml:"allow_reply"`
}

// usersConfig, kullanıcı yetki listesi.
type usersConfig struct {
	Users []userConfig `yaml:"users"`
}

// enabled, en az bir kullanıcı tanımlı mı.
func (u usersConfig) enabled() bool {
	return len(u.Users) > 0
}

// heartbeat, sessiz kalan sohbeti belirli aralıklarla uyarır.
//
// Amaç: "hat açık mı" testi değil, "admin konuşmuyor mu" hatırlatması. Hatın
// sağlığını `ping` kuralı zaten ölçer; bu ikisi karışmasın diye ayrı yazıldı.
type heartbeat struct {
	// To, kontrol edilecek sohbet(ler). LID ya da telefon yazılabilir.
	To stringList `yaml:"to"`

	// SessionID, uyarı verilecek sabit oturum. Boşsa heartbeat kapalıdır.
	SessionID string `yaml:"session"`

	// Idle, bu süre sessiz kalınırsa tetiklenir (ör. "15m").
	Idle string `yaml:"idle"`

	// Every, kontrol sıklığı (ör. "1m").
	Every string `yaml:"every"`

	// Mesaj, oturuma gönderilecek metin.
	Mesaj string `yaml:"mesaj"`
}

// enabled, heartbeat yapılandırılmış mı.
//
// SessionID olmadan çalışmanın anlamı yok: uyarı kime gönderilecek?
func (h heartbeat) enabled() bool {
	return strings.TrimSpace(h.SessionID) != "" && strings.TrimSpace(h.Mesaj) != ""
}

// notifyConfig, tetikleyicinin olay bildirimlerini açıp yapılandırır.
//
// Her on_* bayrağı bir olayı açar. Gönderim yalnız `to` listesine gider —
// aşağıda "kendi hesabımıza asla" notuna bakın.
type notifyConfig struct {
	// To, bildirimlerin gideceği kimlik(ler). LID ya da telefon numarası.
	// Boşsa notifier hiçbir şey göndermez.
	To stringList `yaml:"to"`

	// OnStart, tetikleyici açılınca haber verir.
	OnStart bool `yaml:"on_start"`

	// OnStop, tetikleyici kapanınca haber verir.
	OnStop bool `yaml:"on_stop"`

	// OnBridgeUp, köprü yeniden ulaşılır olunca haber verir.
	OnBridgeUp bool `yaml:"on_bridge_up"`

	// OnBridgeDown, köprüye ulaşılamayınca haber verir.
	OnBridgeDown bool `yaml:"on_bridge_down"`

	// OnConfigChange, config dosyası değişince haber verir.
	OnConfigChange bool `yaml:"on_config_change"`

	// Throttle, aynı tür bildirimin en erken tekrar süresidir ("5m").
	Throttle string `yaml:"throttle"`

	// Retry, gönderim başarısız olunca tekrar deneme aralığı ("30s").
	Retry string `yaml:"retry"`

	// MaxRetry, köprü bağlı değilken en fazla kaç kez deneneceği.
	MaxRetry int `yaml:"max_retry"`
}

// enabled, bildirim mekanizması çalışıyor mu. Kimse yok ya da hiçbir olay
// açık değilse hiçbir şey gönderilmez — config'e notify: bloğu koymak
// için yeterli değildir, en az bir olay da açılmalı.
func (n notifyConfig) enabled() bool {
	if len(n.To) == 0 {
		return false
	}
	return n.OnStart || n.OnStop || n.OnBridgeUp || n.OnBridgeDown || n.OnConfigChange
}

// rule, bir koşul ve koşul tuttuğunda yapılacaklar.
type rule struct {
	// Name yalnız log ve status çıktısı içindir; boş olabilir ama
	// hatayı bulmak için doldurulması önerilir.
	Name string `yaml:"name"`

	// Match, mesajın tutması gereken koşullardır.
	Match condition `yaml:"match"`

	// Actions sırayla çalışır; biri çökerse diğerleri yine çalışır.
	Actions []action `yaml:"do"`

	// Dosyada kaçıncı sırada olduğu. Yükleme sırasında doldurulur ki hata
	// mesajı kullanıcıya satır yerine kural adıyla gitsin.
	index int `yaml:"-"`
}

// condition, bir mesajın tutması gereken koşullardır.
//
// Kimlik (`number` / `name`) ile içerik (`text` / `group`) farklı davranır:
//
//   - Kimlik alanları **öncelikli OR**: önce number denenir, tutmazsa name'e
//     bakılır. Buna ihtiyaç var çünkü LID'li sohbetlerde telefon numarası
//     payload'da hiç yoktur; tek kuralda ikisini birden yazmak, sohbet numara
//     biçiminde açıldığında da LID biçiminde açıldığında da çalışmasını sağlar.
//   - İçerik alanları **AND**: kimlik tutsa bile text tutmuyorsa kural tutmaz.
//
// Hiçbir alan verilmemişse koşul her mesajı tutar.
type condition struct {
	// Everyone açıkça her mesajı tutar. Diğer alanlarla birlikte verilirse
	// bunlar yok sayılır.
	Everyone bool `yaml:"everyone"`

	// Numbers From ya da Sender alanlarında aranır, önceliklidir.
	Numbers stringList `yaml:"number"`

	// WPNumber, Numbers'ın okunur adıdır (WhatsApp number). İkisi de
	// yazılabilir; aynı listeye birleştirilir. Neden: "wp_" öneki alanın
	// nereye ait olduğunu tek bakışta söyler (wp_number / wp_name), eski
	// `number` yazımı ise geriye dönük uyum için durur.
	WPNumber stringList `yaml:"wp_number"`

	// Names, PushName alanında aranır (büyük/küçük harf duyarsız).
	// Numbers da verilmişse yalnız number tutmazsa bakılır.
	Names stringList `yaml:"name"`

	// WPName, Names'in okunur adıdır (WhatsApp name). Numbers gibi birleşir.
	WPName stringList `yaml:"wp_name"`

	// Text mesaj metninde aranır (büyük/küçük harf duyarsız).
	Text string `yaml:"text"`

	// NotText mesaj metninde ARANMAMALIDIR (büyük/küçük harf duyarsız).
	// Text ile birlikte AND'lenir: metin Text'i tutup NotText'i tutuyorsa
	// kural tutmaz. Tek başına verilirse "şu kelime hariç herkes" olur.
	// Kullanımı: ping kuralı anında cevap verirken oturum kuralının aynı
	// mesaja opencode'u çağırmaması için oturuma `not_text: ping` yazılır.
	//
	NotText string `yaml:"not_text"`

	// Types, mesajın tipinde aranır: audio, image, document, text, video…
	// AND koşuludur: "Mefamex'ten gelen sesli not" için number/name ile
	// birlikte yazılır. Liste de olabilir (sesli not VEYA video).
	Types stringList `yaml:"type"`

	// GroupsOnly yalnız grup sohbetlerini tutar.
	GroupsOnly bool `yaml:"group"`

	// Prefix mesaj metninin BAŞLANGICINDA aranır (büyük/küçük harf duyarsız).
	// Örn: prefix: "/" → "/cmd", "/help", "/ping" hepsi tutar.
	// text: alt dizi arar (her yerde); prefix: SADECE başlangıçta.
	Prefix string `yaml:"prefix"`

	// NotPrefix mesaj metninin BAŞLANGICINDA ARANMAMALIDIR (büyük/küçük harf duyarsız).
	// Prefix ile birlikte AND'lenir: metin Prefix'i tutup NotPrefix'i tutuyorsa kural tutmaz.
	// Kullanımı: oturum kuralına `not_prefix: "/"` yazarsan "/cmd", "/help" oturuma düşmez.
	NotPrefix string `yaml:"not_prefix"`
}

// normalize, alan takma adlarını tek kaynağa indirir.
//
// `wp_number` → `number`, `wp_name` → `name`. Eşleştirme kodu tek bir alana
// bakar; iki alanı ayrı ayrı kontrol etmek her koşul fonksiyonuna iki satır
// eklemek ve birini unutmak demekti. Birleştirme yükleme anında yapılır, sonra
// alanlar zaten aynıdır.
func (c *condition) normalize() {
	c.Numbers = append(c.Numbers, c.WPNumber...)
	c.Names = append(c.Names, c.WPName...)
	c.WPNumber = nil
	c.WPName = nil
}

// action, bir kuralın yapacağı tek iş.
type action struct {
	// Kind: command | session | new_session
	Kind string `yaml:"type"`

	// Code, yalnız "command" için: çalıştırılacak kabuk komutu.
	Code string `yaml:"code"`

	// SessionID, yalnız "session" için: bağlanılacak opencode oturumu.
	SessionID string `yaml:"session"`

	// ── İç alanlar (config'ten GELMEZ, yaml:"-") ─────────────────────────
	//
	// DevamMetni: yazılırsa açık prompt yerine bu metin oturuma verilir.
	// Rate limit sonrası otomatik "Continue, unstop" (devamDene) için kod
	// içinde kurulur; kullanıcı config'ine yazılmaz.
	//
	// SessizHata: true ise bu turun hataları kullanıcıya SÖYLENMEZ ve kısmi
	// çıktısı GÖNDERİLMEZ. Otomatik devam denemeleri için: başarısız
	// denemeler sessizdir, sonucu ana akış (tek mesajla) bildirir.
	DevamMetni string `yaml:"-"`
	SessizHata bool   `yaml:"-"`

	// Reply, eylemin çıktısını tetikleyen sohbete WhatsApp mesajı olarak
	// göndermeyi ister. Her eylem türüyle kullanılabilir: komutun stdout'u da
	// ajanın cevabı da sohbete gider.
	//
	// Değer true ise çıktının tamamı, string ise o sabit metin gönderilir.
	Reply replyTarget `yaml:"reply"`

	// Agent, oturumu açarken kullanılacak opencode ajanının adıdır.
	//
	// Neden gerekli: ajanın elinde `whatsapp_send` aracı olursa cevabı
	// KENDİSİ gönderir, tetikleyici de stdout'u gönderir → kişi aynı şeyi
	// iki kez alır. Ölçüm: export/1-mefamex 22:05 "masha'ya niye 2 kere
	// mesaj gonderiyon"; ajanın kendi teşhisi 22:25 "iki kanal çakışıyor".
	//
	// Çözüm yapısal: `wp-sahip` / `wp-misafir` ajanları gönderim aracını
	// opencode izin yapılandırmasında reddeder. Model istese de gönderemez.
	// Tanımlar: ~/.config/opencode/opencode.json → "agent"
	Agent string `yaml:"agent"`

	// Timeout bu tek eylemin üst sınırıdır. Yazılmazsa --action-timeout
	// bayrağı (varsayılan 3m) geçerlidir.
	//
	// Neden gerekli: "1 numaralı ödevi yap" gibi bir iş 3 dakikayı aşabiliyor
	// ve 3 dakikada öldürülen ajanın emeği kayboluyordu. Uzun işler için
	// buraya `timeout: 15m` yazılır; kısa cevaplar 8 saniyede biter, uzun
	// olanlar 15 dakikaya kadar çalışır.
	Timeout string `yaml:"timeout"`

	// ── type: opencode — tek kişi tek kural ────────────────────────────
	//
	// Bu alanlar yalnız `type: opencode` eyleminde okunur. Amaç: bir kişi için
	// beş ayrı kural (metin, sesli not, görsel, belge, komut) yazmak yerine
	// TEK kural yazıp yönlendirmeyi eylemin kendisine bırakmak.
	//
	// Neden: beş kural, aynı numarı beş kez yazmak demekti; birinden birini
	// silmek diğerlerinin eşleşmesini bozuyordu ve "hangi kural ne işe
	// yarıyordu" sorusu her seferinde yeniden cevaplanıyordu.

	// Cmd, "/" ile başlayan komutların çalıştırılacağı betik.
	// Mesajın metni, JID'i ve adı otomatik argüman olarak eklenir:
	//   <cmd> "$TRIGGER_TEXT" "$TRIGGER_JID" "$TRIGGER_NAME"
	// Betiğin argüman listesini kendisinin bilmesi gerekmez.
	Cmd string `yaml:"cmd"`

	// Gorsel, görsel/video/sticker/belge hattının betiği. Mesaj kimliği
	// otomatik argüman olarak eklenir: <gorsel> "$TRIGGER_ID"
	Gorsel string `yaml:"gorsel"`

	// Ses, sesli not hattının betiği. Argüman listesi Gorsel ile aynı.
	Ses string `yaml:"ses"`

	// SendSeen true ise bu eylemin tetiklediği mesaj okundu işaretlenir
	// (mavi tik). Yazılmazsa global `okundu_isaretle` geçerlidir; yazılırsa
	// o bayrağı geçersiz kılar — kişi bazlı izin mümkün olsun diye.
	SendSeen *bool `yaml:"send_seen"`

	// AddAITag, cevabın sonuna "🤖 AI asistanı" eklenip eklenmeyeceğini bu
	// eylem için belirler. Yazılmazsa global `ai_eti_ekle` geçerlidir.
	AddAITag *bool `yaml:"add_aitag"`

	// DoDebug, ajanın ARA MESAJLARINI (düşünce ve tool çıktısı hariç)
	// WhatsApp'a ayrı ayrı gönderir. Varsayılan false: yalnız son cevap gider.
	// Yazılmazsa global `debug_cikti` geçerlidir.
	DoDebug *bool `yaml:"do_debug"`

	// ── type: opencode — izin bayrakları ──────────────────────────────
	//
	// Her bayrak bir yeteneği açar. Kapalı olan yetenek için mesaj
	// SESSİZCE geçilir: ne cevap gider ne de ajan uyarılır. Bu, kapalı hat
	// kuralının değişmez ilkesidir (config.readme.md § "Sessizlik kasıtlıdır").

	AllowHelp   bool `yaml:"allow_help"`   // /help
	AllowCmd    bool `yaml:"allow_cmd"`    // /cmd ve diğer /… komutları
	AllowDurum  bool `yaml:"allow_durum"`  // /durum
	AllowCamera bool `yaml:"allow_camera"` // /camera (ayrıca kamera_izni gerekir)

	AllowGorsel   bool `yaml:"allow_gorsel"`   // fotoğraf · video · sticker
	AllowSes      bool `yaml:"allow_ses"`      // sesli not
	AllowBelge    bool `yaml:"allow_belge"`    // PDF · DOCX · XLSX
	AllowReaction bool `yaml:"allow_reaction"` // emoji tepkisi
}

// sure, eylemin kendi zaman aşımını çözer: eylem alanı > bayrak > varsayılan.
//
// Yazım hatası sessizce yutulmaz. "timeout: 15" (birim yok) yazan kullanıcı
// 15 saniye beklerken işinin 3 dakikada öldürülmesini beklememelidir; ayrıca
// "15m" yazıp gerçekten 15 dakika uygulandığını görebilmelidir.
func (a action) sure(opts options) time.Duration {
	if a.Timeout == "" {
		return opts.actionTimeout
	}

	d, err := time.ParseDuration(a.Timeout)
	if err != nil {
		logf("  ⚠️  timeout %q geçersiz (%v) → varsayılan %s. Örnek: timeout: 15m",
			a.Timeout, err, opts.actionTimeout)
		return opts.actionTimeout
	}
	if d <= 0 {
		logf("  ⚠️  timeout %q sıfırdan küçük → varsayılan %s",
			a.Timeout, opts.actionTimeout)
		return opts.actionTimeout
	}

	return d
}

// replyTarget, eylemin çıktısını nereye göndereceğini tutar.
type replyTarget struct {
	// On, yanıt gönderilsin mi.
	On bool

	// Text verilmişse çıktı yerine bu sabit metin gönderilir.
	Text string
}

// UnmarshalYAML, hem true hem bir metin kabul eder: kullanıcı "yanıt ver" demek
// istediğinde true, sabit bir cevap göndereceğinde metin yazsın.
func (r *replyTarget) UnmarshalYAML(value *yaml.Node) error {
	// Ölçek çözülmemişse alt düğüme in; stringList'te kullanılan desen.
	var resolved yaml.Node
	if err := value.Decode(&resolved); err != nil {
		return err
	}

	if resolved.Kind == yaml.ScalarNode {
		// "true" / "false" yazılmışsa bayrak, başka bir şeyse metin sayılır.
		var flag bool
		if err := resolved.Decode(&flag); err == nil && isBoolLiteral(resolved.Value) {
			r.On = flag
			return nil
		}

		// Numbers are not valid replies: YAML coerces 42 to "42" on string
		// decode, which would silently accept garbage. Reject by tag.
		if resolved.Tag == "!!int" || resolved.Tag == "!!float" {
			return fmt.Errorf("reply must be true, false, or a string, not a number")
		}

		var text string
		if err := resolved.Decode(&text); err == nil {
			r.On = true
			r.Text = text
			return nil
		}
	}

	return fmt.Errorf("reply true ya da metin olmalı")
}

// isBoolLiteral, bir metnin YAML'in gerçek boolean yorumlarından biri olup
// olmadığını söyler. "evet" gibi bir kelime metindir, bayrak değil.
func isBoolLiteral(s string) bool {
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off":
		return true
	}
	return false
}

// actionKinds, çalıştırılabilir eylem türleri.
var actionKinds = map[string]bool{
	"command":     true,
	"session":     true,
	"new_session": true,
	"opencode":    true,
}

// yetenek, izin bayraklarının okunabilir adıdır — log ve hata mesajı için.
type yetenek struct {
	ad   string
	acik bool
}

// yetenekler, `type: opencode` eyleminin izin bayraklarını sırayla döndürür.
func (a action) yetenekler() []yetenek {
	return []yetenek{
		{"/help", a.AllowHelp},
		{"/cmd", a.AllowCmd},
		{"/durum", a.AllowDurum},
		{"/camera", a.AllowCamera},
		{"görsel/video/sticker", a.AllowGorsel},
		{"sesli not", a.AllowSes},
		{"belge", a.AllowBelge},
		{"reaksiyon", a.AllowReaction},
	}
}

// acikYetenekler, açık olan bayrakların adlarını virgülle döndürür.
func (a action) acikYetenekler() string {
	var adlar []string
	for _, y := range a.yetenekler() {
		if y.acik {
			adlar = append(adlar, y.ad)
		}
	}
	if len(adlar) == 0 {
		return "(hiçbiri)"
	}
	return strings.Join(adlar, ", ")
}

// check validates the action and reports which field is missing by name.
func (a action) check() error {
	if !actionKinds[a.Kind] {
		return fmt.Errorf("unknown action type %q (command, session, new_session, opencode allowed)", a.Kind)
	}
	if a.Kind == "command" && strings.TrimSpace(a.Code) == "" {
		return fmt.Errorf("code required")
	}
	if a.Kind == "session" && strings.TrimSpace(a.SessionID) == "" {
		return fmt.Errorf("session required (find with opencode session list)")
	}
	if a.Kind == "new_session" && strings.TrimSpace(a.SessionID) != "" {
		// new_session bilerek oturum bağlamaz. Yazılmışsa kullanıcı ya
		// yanlış tür seçmiş ya da geçmişten kalan bir alan. İkisi de
		// sessizce yanlış davranış üretir.
		return fmt.Errorf("new_session must not pin a session; remove \"session\" or use type: session")
	}
	if a.Kind == "opencode" {
		return a.checkOpencode()
	}
	return nil
}

// checkOpencode, tek kural eyleminin eksiklerini tek tek isimleriyle bildirir.
//
// Sessizce geçmek yerine config hatası vermek bilinçli bir tercih: beş kuralın
// yarısı yanlış yazıldığında kullanıcı "bazı mesajlarım çalışmıyor" diye
// arıyordu; tek kuralda eksik alan bellidir ve adıyla söylenir.
func (a action) checkOpencode() error {
	if strings.TrimSpace(a.SessionID) == "" {
		return fmt.Errorf("session required (opencode her yazılı mesajı bu oturuma verir)")
	}

	// Kapalı hiçbir yetenek yoksa kural hiçbir işe yaramaz; bu bir yazım
	// hatası, kasıtlı bir kapatma değil.
	var acik int
	for _, y := range a.yetenekler() {
		if y.acik {
			acik++
		}
	}
	if acik == 0 {
		return fmt.Errorf("at least one allow_* must be true (nothing to do otherwise)")
	}

	// Açık yeteneğin betiği yazılmamışsa o hat çalışmaz.
	if strings.TrimSpace(a.Cmd) == "" && (a.AllowHelp || a.AllowCmd || a.AllowDurum || a.AllowCamera) {
		return fmt.Errorf("cmd required (allow_help/allow_cmd/allow_durum/allow_camera need a script)")
	}
	if strings.TrimSpace(a.Gorsel) == "" && (a.AllowGorsel || a.AllowBelge) {
		return fmt.Errorf("gorsel required (allow_gorsel/allow_belge need a script)")
	}
	if strings.TrimSpace(a.Ses) == "" && a.AllowSes {
		return fmt.Errorf("ses required (allow_ses needs a script)")
	}

	return nil
}

// label, eylemi tek satırda okunabilir biçimde gösterir.
func (a action) label() string {
	base := ""
	switch a.Kind {
	case "command":
		base = "command: " + truncate(a.Code, 60)
	case "session":
		base = "session: " + a.SessionID
	case "opencode":
		base = "opencode: " + a.SessionID + " [" + a.acikYetenekler() + "]"
	default:
		base = "new_session"
	}

	if a.Reply.On {
		base += "  → reply"
	}
	return base
}

// name, kuralın loglarda görünecek adı. Adsız kural sıra numarasıyla görünür.
func (r rule) name() string {
	if trimmed := strings.TrimSpace(r.Name); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("#%d", r.index+1)
}

// expandHome, baştaki ~ işaretini kullanıcının ev dizinine çevirir, böylece
// config taşınabilir yazılabilir.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

// loadConfig, config dosyasını okur ve doğrular.
//
// Returns error if file missing: caller distinguishes "no rules" from "broken config"
// arasında ayırt etmek için kullanır.
func loadConfig(path string) (ruleSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ruleSet{}, fmt.Errorf("config unreadable (%s): %w", path, err)
	}
	return parseConfig(raw)
}

// parseConfig, parses and validates the config YAML.
//
// Parsing and validation are separate: a YAML-valid but unexecutable
// rule is still broken config — the user must understand the error without
func parseConfig(raw []byte) (ruleSet, error) {
	var set ruleSet

	// KnownFields prevents silent typos. If user writes "numera:" instead of
	// "number:", the decoder will error instead of silently ignoring it.
	//
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)

	if err := decoder.Decode(&set); err != nil {
		return ruleSet{}, fmt.Errorf("config unreadable: %w", err)
	}

	for i := range set.Rules {
		set.Rules[i].index = i
		name := set.Rules[i].name()

		set.Rules[i].Match.normalize()

		if len(set.Rules[i].Actions) == 0 {
			return ruleSet{}, fmt.Errorf("rule %q: at least one action required", name)
		}
		for j, act := range set.Rules[i].Actions {
			if err := act.check(); err != nil {
				return ruleSet{}, fmt.Errorf("rule %q, action %d: %w", name, j+1, err)
			}
		}
	}

	return set, nil
}
