// whatsapp-bridge-project/tetikleyici/zamanlanmis.go
//
// Zamanlanmış mesaj: "şu zaman sonra şu mesajı gönder" (`/zamanla <süre>
// <metin>`). Gönderim, kısa ömürlü komut sürecinin değil, tetikleyicinin
// kendisinin işidir — bu dosya o uzun ömürlü tarayıcıyı barındırır.
//
// Neden iş dizini (her iş ayrı dosya)? Komut tarafı (/zamanla → cmd.py) iş
// EKLER, tarayıcı gönderince SİLER. Tek ortak dosyada iki yazar yarışırdı;
// ayrı dosyalarda atomik yaz/sil ile çakışma yok. İşler restart'a dayanır:
// dosyalar .tetikleyici/zamanlanmis/ altında durur.
//
// Hata politikası: gönderilemeyen iş 1 dakika sonra yeniden denenir, 3
// başarısız denemede iptal edilir (logla düşürülür) — köprü düşükken 5
// saniyede bir spam denemek anlamsız. Köprüsüz kip (client nil) işlere
// dokunmaz.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// zamanlanmisJob, "şu saatte şu mesajı gönder" işidir.
type zamanlanmisJob struct {
	ID        int64  `json:"id"`
	To        string `json:"to"`
	Text      string `json:"text"`
	FireAt    int64  `json:"fire_at"`    // Unix saniye (gönderim zamanı)
	Created   int64  `json:"created"`    // Unix saniye
	Deneme    int    `json:"deneme"`     // başarısız deneme sayısı
	SonDeneme int64  `json:"son_deneme"` // son deneme Unix saniyesi (0 = yok)
}

const (
	zamanlanmisMaxDeneme = 3               // gönderilemeyen işin azami denemesi
	zamanlanmisDenemeAra = time.Minute     // başarısız denemeler arası nefes
	zamanlanmisTaramaAra = 5 * time.Second // iş dizini tarama sıklığı
)

// zamanlanmisDizin, iş dosyalarının dizini (ör. .tetikleyici/zamanlanmis/).
func zamanlanmisDizin() string {
	return defaultState("zamanlanmis")
}

// zamanlanmisEnv, komut süreçlerine (cmd.py) zamanlanmış mesaj bayrağını ve
// iş dizinini taşır. Kapalıyken bile dizini iletir ki komut "kapalı" cevabını
// verebilsin.
func zamanlanmisEnv(opts options) []string {
	acik := "0"
	if opts.zamanlanmisAcik {
		acik = "1"
	}
	return []string{
		"ZAMANLANMIS_ACIK=" + acik,
		"ZAMANLANMIS_DIZIN=" + zamanlanmisDizin(),
	}
}

// zamanlanmisIzle, zamanlanmış mesajları yöneten döngüdür. run() tarafından
// goroutine olarak çalıştırılır; köprü bağlı olmasa bile durmaz — bağlantı
// geri gelince işler yerinde durur.
//
// acik, çalışma anındaki bayrağı döndüren kapıdır (engine.zamanlanmisAcikDurum).
// MEVZU Aşama 3 (ADIM 3): tarayıcı HER ZAMAN çalışır ama kapalıyken tur
// ATMaz — böylece /zamanla aç çalışma anında tarayıcıyı canlandırır
// (önceden yalnız açık başlangıçta başlıyordu → açılan işler hiç
// gönderilmiyordu). Kapalıyken bekleyen işler dizinde kalır; tekrar
// açılınca vadesi geçenler hemen gider.
func zamanlanmisIzle(ctx context.Context, client *bridgeClient, acik func() bool) {
	if err := os.MkdirAll(zamanlanmisDizin(), 0o700); err != nil {
		logf("  zamanlanmış iş dizini kurulamadı (%v)", err)
		return
	}
	ticker := time.NewTicker(zamanlanmisTaramaAra)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if acik != nil && !acik() {
				continue
			}
			zamanlanmisTur(ctx, client, zamanlanmisDizin())
		}
	}
}

// zamanlanmisTur, bir tarama turunu işler: vadesi gelen işleri gönderir,
// gönderilenleri siler. Başarısız işler SİLİNMEZ — deneme sayısı artar ve
// nefes (zamanlanmisDenemeAra) dolunca yeniden denenir; 3 başarısız denemede
// iptal edilir. Ayrı fonksiyon = test edilebilir.
func zamanlanmisTur(ctx context.Context, client *bridgeClient, dizin string) {
	jobs := zamanlanmisOku(dizin)
	if len(jobs) == 0 {
		return
	}

	simdi := time.Now().Unix()
	for _, j := range jobs {
		// Vadesi gelmemiş işe dokunma.
		if j.FireAt > simdi {
			continue
		}
		// Başarısız denemeler arasında nefes: bir dakikadan erken tekrarlama.
		if j.Deneme > 0 && simdi-j.SonDeneme < int64(zamanlanmisDenemeAra.Seconds()) {
			continue
		}
		if client == nil {
			// Köprüsüz kip: işe dokunma — gönderilemez, silmek yanlış olur.
			continue
		}

		if err := client.sendText(ctx, j.To, j.Text); err != nil {
			j.Deneme++
			j.SonDeneme = simdi
			if j.Deneme >= zamanlanmisMaxDeneme {
				logf("  ⚠ zamanlanmış mesaj #%d GÖNDERİLEMEDİ (%d deneme), iptal: %v", j.ID, j.Deneme, err)
				_ = zamanlanmisSil(dizin, j.ID)
				continue
			}
			logf("  ⚠ zamanlanmış mesaj #%d gönderilemedi (%d/%d): %v", j.ID, j.Deneme, zamanlanmisMaxDeneme, err)
			_ = zamanlanmisKaydet(zamanlanmisJobYol(dizin, j.ID), j)
			continue
		}
		logf("  📅 zamanlanmış mesaj #%d gönderildi (%d karakter)", j.ID, len(j.Text))
		_ = zamanlanmisSil(dizin, j.ID)
	}
}

// zamanlanmisOku, dizindeki tüm iş dosyalarını okur (isim sırasıyla).
func zamanlanmisOku(dizin string) []zamanlanmisJob {
	desen := filepath.Join(dizin, "*.json")
	seyler, err := filepath.Glob(desen)
	if err != nil {
		return nil
	}
	var jobs []zamanlanmisJob
	for _, yol := range seyler {
		ham, err := os.ReadFile(yol)
		if err != nil {
			continue
		}
		var j zamanlanmisJob
		if err := json.Unmarshal(ham, &j); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	return jobs
}

// zamanlanmisSil, bir iş dosyasını siler. Bulunamazsa sessizce geçer.
func zamanlanmisSil(dizin string, id int64) error {
	err := os.Remove(zamanlanmisJobYol(dizin, id))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// zamanlanmisJobYol, bir işin dosya yoludur (ör. .../zamanlanmis/1728.json).
func zamanlanmisJobYol(dizin string, id int64) string {
	return filepath.Join(dizin, fmtZamanlanmisID(id))
}

// zamanlanmisKaydet, bir işi atomik yazar (temp + rename): tarayıcı hiçbir
// zaman yarım dosya görmez. Kaydeden tek yazar iş başına kendisi olduğundan
// (iş dosyası ya komut tarafından yeni yazılır ya tarayıcı tarafından
// güncellenir/silinir) çakışma yoktur.
func zamanlanmisKaydet(yol string, j zamanlanmisJob) error {
	veri, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, veri, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, yol)
}

// fmtZamanlanmisID, iş dosyası adını üretir (ör. 1728.json).
func fmtZamanlanmisID(id int64) string {
	return strconv.FormatInt(id, 10) + ".json"
}
