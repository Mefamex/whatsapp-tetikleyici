// whatsapp-bridge-project/tetikleyici/cift_test.go
//
// ÇİFT GÖNDERİM DENETİMİNİN CANLI ÖLÇÜMÜ.
//
// TODO'da açık kalan madde: "gerçek mesaj sayımı yapılmadı, testler sahte
// köprüyle geçiyor". Bu dosya SAHTE KÖPRÜ KULLANMAZ — gerçek köprüye
// (127.0.0.1:31415) bağlanır, gerçekten mesaj gönderir, gerçek geçmişten
// okur ve "bu metin az önce gitti mi" sorusunu gerçek HTTP yolundan yanıtlar.
//
// NEDEN BU KADAR ÖNEMLİ
// Çift gönderim denetimi üç şeye dayanır ve üçü de SAHTE KÖPRÜDE YANLIŞ
// ÇIKABİLİR:
//   1. Köprü zaman damgaları SANİYE cinsinden. Sahte köprüde test, nanosecond
//      ya da Unix(0) koyabiliyor; gerçekte saniye.
//   2. Geçmişin SIRASI. Sahte köprü testte istediği sırayı verir.
//   3. fromMe() AYRIMI. Giden mesaj gerçekten fromMe=true mi, yoksa köprü
//      normalize ederken mi bozuluyor?
//
// Bu dosya o üçünü de GERÇEK yoldan ölçer.
//
// ⚠️ KİMLİK KURALI: yalnız mefamex'e (12345678901234@lid) gönderir. Başka kimseye
// ve gruba mesaj gitmez. Test canlı köprüye yazdığı için buradaki metinler
// gerçekten gönderilir — bu yüzden kısa ve anlamsız tutulur.
//
// Bu dosya SKIP edilir: BRIDGE_CANLI_TEST=1 verilmemişse.

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// canliNumara, bu testlerin gönderebileceği TEK sohbet: mefamex.
//
// Kural: başka kimseye ve gruba mesaj gitmez. Sabit olarak burada duruyor,
// çağıran kodu değiştiremez (yanlış numaraya göndermeyi zorlaştırmak için).
const canliNumara = "12345678901234@lid"

// canliKopruAdresi, gerçek köprünün adresi (main.go'daki varsayılanla aynı).
const canliKopruAdresi = "http://127.0.0.1:31415"

// canliKopruKur, gerçek köprüye bağlanan bir istemci kurar.
//
// Zaman aşımı kısa tutulur: canlı köprü yoksa test 30 saniye bekleyip
// "bağlanamadı" deyip geçmeli, 2 dakika sonra değil.
func canliKopruKur(t *testing.T) *bridgeClient {
	t.Helper()

	if os.Getenv("BRIDGE_CANLI_TEST") != "1" {
		t.Skip("canlı test: BRIDGE_CANLI_TEST=1 ile açılır")
	}

	ctx, iptal := context.WithTimeout(context.Background(), 10*time.Second)
	defer iptal()

	client := newBridgeClient(canliKopruAdresi, 20*time.Second)

	// Sağlık: adres doğru olmak yetmez, köprü BAĞLI olmalı. Bağlantısız
	// köprüde gönderim sessizce boşa gider (NOTES.md kuralı) ve test
	// "başarılı" görünür — en tehlikeli hata.
	if err := client.health(ctx); err != nil {
		iptal()
		t.Fatalf("köprü bağlı değil (%s): %v", canliKopruAdresi, err)
	}
	return client
}

// TestCiftGonderimCanliYeniMesajiYeniSaymaz — gönderilen bir metin, az önce
// GİTMEDİĞİ için bastırılmamalı.
//
// Bu, denetimin en tehlikeli yönü: yanlış çalışırsa kullanıcının cevabı
// sessizce yutulur. Kullanıcı "yanıt yok" der, nedenini bulamaz.
func TestCiftGonderimCanliYeniMesajiYeniSaymaz(t *testing.T) {
	client := canliKopruKur(t)
	ctx := context.Background()

	metin := "cift-gonderim denetimi: benzersiz işaret " + time.Now().Format("15:04:05.000")

	aynen, sebep := zatenGonderilmis(ctx, client, canliNumara, metin)
	if aynen {
		t.Fatalf("HİÇ gönderilmemiş bir metin 'zaten gönderilmiş' sayıldı: %s", sebep)
	}
}

// TestCiftGonderimCanliGonderilenMesajiGoruyor — gönderilen bir metin, az sonra
// bastırılmalı. Bu, denetimin çalıştığının kanıtı.
//
// Sahte köprüde bu test "çalışır" ama gerçeği ölçmez: sahte köprü
// history'yi testin istediği gibi döndürür. Burada mesaj GERÇEKTEN gönderilir
// ve geçmişten GERÇEKTEN okunur.
func TestCiftGonderimCanliGonderilenMesajiGoruyor(t *testing.T) {
	client := canliKopruKur(t)
	ctx := context.Background()

	isaret := "cift-gonderim denetimi " + time.Now().Format("15:04:05.000")
	metin := isaret

	if err := client.sendText(ctx, canliNumara, metin); err != nil {
		t.Fatalf("gönderilemedi: %v", err)
	}

	// Köprüye mesajın geçmesi zaman alır. Sabit bekleme YANLIŞ olurdu:
	// hızlı bir köprüde gereğinden uzun, yavaş bir köprüde yetersiz kalır.
	// Ölçüt: mesaj geçmişte GÖRÜNENE kadar bekle.
	aynen := false
	var sebep string
	for deneme := 0; deneme < 20; deneme++ {
		aynen, sebep = zatenGonderilmis(ctx, client, canliNumara, metin)
		if aynen {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !aynen {
		t.Fatalf("gönderilen mesaj geçmişten okunduğu hâlde 'yeni' sayıldı (%s).\n"+
			"Bu, çift gönderim denetimini TAMAMEN ETKİSİZ hâle getirir:\n"+
			"her yeniden denemede aynı cevap tekrar gönderilir.", sebep)
	}
}

// TestCiftGonderimCanliZamanDamgasiSaniyeCinsinden — köprü zaman damgaları
// saniye cinsinden; denetim `time.Unix(ts,0)` ile çözüyor.
//
// ÖLÇÜM: gönderimden sonra geçen süre, geçmişteki zaman damgası farkıyla
// TOLERANS İÇİNDE örtüşmeli. Birimi yanlışsa (ör. milisaniye) fark 1000 kat
// büyük çıkar ve pencerenin dışına düşer → denetim hiç çalışmaz.
func TestCiftGonderimCanliZamanDamgasiSaniyeCinsinden(t *testing.T) {
	client := canliKopruKur(t)
	ctx := context.Background()

	metin := "zaman-damani denetimi " + time.Now().Format("15:04:05.000")
	gonderme := time.Now()

	if err := client.sendText(ctx, canliNumara, metin); err != nil {
		t.Fatalf("gönderilemedi: %v", err)
	}

	var hedef time.Time
	var bulundu bool
	for deneme := 0; deneme < 20; deneme++ {
		msgs, err := client.history(ctx, canliNumara, 12)
		if err != nil {
			t.Fatalf("geçmiş okunamadı: %v", err)
		}
		for _, m := range msgs {
			if m.fromMe() && teklastirMetin(m.Text) == teklastirMetin(metin) {
				hedef, bulundu = time.Unix(m.Timestamp, 0), true
				break
			}
		}
		if bulundu {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !bulundu {
		t.Fatal("gönderilen mesaj geçmişte bulunamadı")
	}

	fark := gonderme.Sub(hedef).Abs()

	// Saniye cinsinden bir damga, gönderim anından en fazla ~2 saniye
	// uzakta olabilir (ağ + tampon). Dakika saat olsaydı bu fark yüzlerce
	// saniye olurdu ve test kırmızıya dönerdi.
	if fark > 5*time.Second {
		t.Errorf("zaman damgası %v uzakta — saniye cinsinden DEĞİL?\n"+
			"Denetim `yas > pencere+1sn` ile eleyeceği için sessizce\n"+
			"her zaman 'yeni' sanır ve çift gönderim koruması ölür.", fark)
	}
}

// TestCiftGonderimCanliBicimKabugu — model aynı cevabı "**Toplam 23 TL**" ve
// "Toplam 23 TL" biçimlerinde verebilir. Bunlar AYNI cevaptır.
//
// Bu saf metin normalizasyonu, ama canlı köprü üzerinden ölçülür: köprünün
// metni bozmadığı (emoji, satır sonu, format kabuğu) da görülür.
func TestCiftGonderimCanliBicimKabugu(t *testing.T) {
	client := canliKopruKur(t)
	ctx := context.Background()

	ham := "kabuk denetimi " + time.Now().Format("15:04:05.000")
	if err := client.sendText(ctx, canliNumara, "**"+ham+"**"); err != nil {
		t.Fatalf("gönderilemedi: %v", err)
	}

	var bulundu bool
	for deneme := 0; deneme < 20; deneme++ {
		msgs, err := client.history(ctx, canliNumara, 12)
		if err != nil {
			t.Fatalf("geçmiş okunamadı: %v", err)
		}
		for _, m := range msgs {
			if m.fromMe() && teklastirMetin(m.Text) == teklastirMetin(ham) {
				bulundu = true
				break
			}
		}
		if bulundu {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !bulundu {
		t.Fatal("biçim kabuğu soyulduğunda metin eşleşmedi — aynı cevap iki kez sayılır")
	}
}

// TestCiftGonderimCanliGelenMesajiSaymaz — KULLANICININ mesajı bastırma
// sebebi OLMAMALI. Yalnız kendi gönderimlerimiz sayılır.
//
// Bu bir canlı tuzak: köprü bize gelen mesajı fromMe=false döndürürse,
// kullanıcı bir şey sorduğunda aynı soru "zaten gönderilmiş" sayılır ve
// ajan hiç çalışmaz. Kullanıcı "soru yazdım, cevap yok" der.
func TestCiftGonderimCanliGelenMesajiSaymaz(t *testing.T) {
	client := canliKopruKur(t)
	ctx := context.Background()

	msgs, err := client.history(ctx, canliNumara, 12)
	if err != nil {
		t.Fatalf("geçmiş okunamadı: %v", err)
	}
	if len(msgs) == 0 {
		t.Skip("geçmiş boş — bu test için mesaj gerekir")
	}

	var gelen []string
	for _, m := range msgs {
		if !m.fromMe() {
			gelen = append(gelen, m.Text)
		}
	}
	if len(gelen) == 0 {
		t.Skip("geçmişte gelen mesaj yok — canlı doğrulama yapılamadı")
	}

	// Kullanıcının METNİYLE bayt bayt aynı olan kendi gönderimimiz olmamalı.
	for _, metin := range gelen {
		if strings.TrimSpace(metin) == "" {
			continue
		}
		aynen, sebep := zatenGonderilmis(ctx, client, canliNumara, metin)
		if aynen {
			t.Errorf("kullanıcının mesajı 'zaten gönderilmiş' sayıldı: %s\n"+
				"Kullanıcı bir şey sorduğunda ajan hiç çalışmaz.", sebep)
		}
	}
}

// TestCiftGonderimCanliBosMetinYokSayilir — boş metin asla "gönderilmiş" sayılmaz.
//
// Boş metin her sohbette eşleşirdi (teklastirMetin("") == ""), yani bir
// hatadaki boş yanıt, sonraki TÜM boş yanıtları bastırırdı.
func TestCiftGonderimCanliBosMetinYokSayilir(t *testing.T) {
	client := canliKopruKur(t)

	for _, bos := range []string{"", "   ", "\n\t "} {
		aynen, sebep := zatenGonderilmis(context.Background(), client, canliNumara, bos)
		if aynen {
			t.Errorf("boş metin (%q) 'zaten gönderilmiş' sayıldı: %s", bos, sebep)
		}
	}
}
