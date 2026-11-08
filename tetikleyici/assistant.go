// whatsapp-bridge-project/tetikleyici/assistant.go
//
// Tek kişi tek kural: `type: opencode` eylemi.
//
// Sorun: bir kişi için ayrı kurallar yazılınca aynı kimlik her kuralda tekrar
// eder (metin, sesli not, görsel, belge, komut = 5 kural). Yeni bir yetenek
// eklemek yeni kural demekti; silmek diğerlerinin eşleşmesini bozuyordu ve
// "hangi kural ne işe yarıyordu" sorusu her seferinde yeniden cevaplanıyordu.
//
// Çözüm: eşleştirme KİMİN geldiğine bakar (tek kural), yönlendirme NE
// geldiğine bakar (eylemin kendi içinde). Yeni yetenek artık yeni kural değil,
// mevcut kurala eklenen bir `allow_*` bayrağı.

package main

import "strings"

// sessizTurler, yönlendirmeye girmeyen iç trafik türleridir.
//
// Bunlar kullanıcının yazdığı mesajlar değil, köprünün protokol olaylarıdır:
// geri alma, geçmiş senkronizasyonu, çözülemeyen payload. Bunları ajana
// vermek ajanı anlamsız işlerle meşgul eder; yönlendirme dışında bırakılırlar.
var sessizTurler = map[string]bool{
	"protocol":    true,
	"poll_update": true,
	"noPayload":   true,
	"unknown":     true,
}

// metinTurleri, ajanın oturumuna giden yazılı mesaj türleridir.
//
// `poll` bilerek listeye girdi: köprü anketi okunabilir metne çevirir
// ("Anket: Pıttık mı? • A • B"), bu da ajana gitmesi gereken bir bilgidir.
// `poll_update` (kullanıcı oy kullandı) iç durum değişimidir, gürültü olurdu —
// o yüzden sessizTurler'de.
var metinTurleri = map[string]bool{
	"text":          true,
	"extended_text": true,
	"poll":          true,
}

// yol, tek kuralın bu mesaj için hangi hattı çalıştıracağını söyler.
type yol struct {
	// ad, günlükte okunan kısa isim ("oturum", "görsel hattı", "/help").
	ad string

	// tip: "session" → opencode oturumu, "command" → betik.
	tip string

	// betik, tip "command" ise çalıştırılacak yol.
	betik string

	// medya true ise betik mesaj kimliği bekler ("$TRIGGER_ID"), metin beklerse
	// metin/JID/ad verilir.
	//
	// Neden yol bilgisi: görsel ve sesli not aynı türde olmadığı için betiğin
	// argümanı hatta göre değişiyor. Argümanı "hangi betik" sorusuna cevap
	// vererek (betik adı eşleşmesiyle) belirlemek kırılgandı: iki hat aynı
	// betiği paylaşabildiğinde biri diğerinin argümanını alıyordu.
	medya bool
}

// acikMi, yönlendirilip yönlendirilemeyeceğini bildirir.
func (y yol) acikMi() bool { return y.tip != "" }

// script, betiğe otomatik eklenen argümanlarla komut satırını kurar.
//
// Argümanlar config'de YAZILMAZ. Neden: `{"$TRIGGER_TEXT"}` yazmak hatırlanması
// gereken bir ayrıntıydı ve bir kez ters yazıldığında (medya yerine metin) hata
// mesajı çıktı mı çıkmadı mı belli olmuyordu. Eylem hangi hatı açtığını zaten
// biliyor, argümanı da o bilmelidir.
func (a action) script(hat yol) string {
	betik := strings.TrimSpace(hat.betik)
	if betik == "" {
		return ""
	}
	if hat.medya {
		return betik + ` "$TRIGGER_ID"`
	}
	return betik + ` "$TRIGGER_TEXT" "$TRIGGER_JID" "$TRIGGER_NAME"`
}

// yonlendir, mesajın gideceği hattı seçer.
//
// Dönüş: (yol, bulundu). Bulunmadıysa mesaj SESSİZCE geçilir — ne cevap
// gönderilir ne de ajan uyarılır. Bu, kapalı hat kuralının değişmez ilkesidir.
func (a action) yonlendir(m message) (yol, bool) {
	if sessizTurler[m.Type] {
		return yol{}, false
	}

	// Tip boş olabilir ve İÇERİK yine vardır. Köprü bugün her mesaja tip
	// koyuyor ama bu alan onun çıktısına bağlı: alan çıkarılırsa ya da yeni
	// bir mesaj türü tip alanı doldurmadan gelirse, içerikli mesajı sessizce
	// yutmak "ajan bazı mesajları görmüyor" hatası üretirdi.
	//
	// Bu yüzden tip yoksa metne bakılır: içerik varsa yazı sayılır.
	tipYazi := metinTurleri[m.Type] || (m.Type == "" && strings.TrimSpace(m.Text) != "")

	switch {
	case tipYazi:
		// "/" ile başlayan metin AJANIN İŞİ DEĞİL — komut hattına aittir.
		//
		// Kapalı bir komutun oturuma düşmemesi şart: `/camera` izni kapalıyken
		// bu metin ajana soru olarak gider ve ajan "bu komut yok" diye
		// cevap verir. Kullanıcı komutu kapatmıştı, karşılığında da
		// karşılık almamak istiyor.
		if komutMu(m.Text) {
			return a.komutYolu(m.Text)
		}
		// CMDHUB kontrolü: kullanıcı cmdhub tünelindeyse ve mesaj "/" komutu değilse
		// doğrudan komut yürütücüye (cmd.py) iletilir.
		if mod, _ := tunelDurumuAl(m.From); mod == "cmdhub" {
			return yol{ad: "cmdhub hattı", tip: "command", betik: a.Cmd}, true
		}
		return yol{ad: "oturum", tip: "session"}, true

	case m.Type == "reaction":
		if a.AllowReaction {
			return yol{ad: "reaksiyon → oturum", tip: "session"}, true
		}
		return yol{}, false

	case m.Type == "poll_vote":
		return yol{ad: "anket oyu → oturum", tip: "session"}, true

	case m.Type == "audio":
		if a.AllowSes {
			return yol{ad: "ses hattı", tip: "command", betik: a.Ses, medya: true}, true
		}
		return yol{}, false

	case m.Type == "document":
		if a.AllowBelge {
			return yol{ad: "belge hattı", tip: "command", betik: a.Gorsel, medya: true}, true
		}
		return yol{}, false

	case m.Type == "image", m.Type == "video", m.Type == "sticker":
		if a.AllowGorsel {
			return yol{ad: m.Type + " hattı", tip: "command", betik: a.Gorsel, medya: true}, true
		}
		return yol{}, false
	}

	return yol{}, false
}

// komutMu, metnin bir komut olup olmadığını söyler.
//
// Ayrı bir fonksiyon çünkü yönlendirme iki yerde aynı soruyu soruyor: "bu
// komut hattına mı gider?" ve "izin kapalıysa ne yapmalıyım?".
func komutMu(metin string) bool {
	return strings.HasPrefix(strings.TrimSpace(metin), "/")
}

// komutYolu, "/" ile başlayan bir metni komut hattına yönlendirir.
//
// Her komut AYRI bir izin bayrağına bağlıdır. Neden ayrı: `/camera` bilgisayarın
// kamerasını açar, `/help` yalnız metin döndürür. Tek bir `allow_cmd` ikisini
// birlikte açıp kapatırdı ve kapatılan şeyin ne olduğu belirsizleşirdi.
//
// İzin kapalıysa ikinci dönüş değeri false'tur: çağıran taraf bunu "sessizce
// geç" olarak yorumlar, oturuma düşürmez.
func (a action) komutYolu(metin string) (yol, bool) {
	if !komutMu(metin) {
		return yol{}, false
	}

	ilk := ilkKelime(metin)
	var acik bool
	switch strings.ToLower(ilk) {
	case "/help":
		acik = a.AllowHelp
	case "/camera":
		acik = a.AllowCamera
	case "/durum":
		acik = a.AllowDurum
	default:
		// /cmd ve config'de henüz bilinmeyen her "/" komutu.
		acik = a.AllowCmd
	}

	if !acik {
		return yol{}, false
	}
	return yol{ad: ilk, tip: "command", betik: a.Cmd}, true
}

// ilkKelime, metnin başındaki boşluktan önceki parçayı verir.
//
// "/camera" → "/camera", "/cmd ls -la" → "/cmd", "  /help" → "/help".
func ilkKelime(metin string) string {
	metin = strings.TrimSpace(metin)
	if i := strings.IndexAny(metin, " \t"); i >= 0 {
		return metin[:i]
	}
	return metin
}

// aciklama, eylemin tek satırlık özetini döndürür — durum çıktısı ve günlük için.
func (a action) aciklama() string {
	var acik, kapali []string
	for _, y := range a.yetenekler() {
		if y.acik {
			acik = append(acik, y.ad)
		} else {
			kapali = append(kapali, y.ad)
		}
	}
	out := "açık: " + strings.Join(acik, ", ")
	if len(kapali) > 0 {
		out += " · kapalı: " + strings.Join(kapali, ", ")
	}
	return out
}
