// whatsapp-bridge-project/tetikleyici/isleyici.go
//
// Mesaj işleyiş düzeni: sohbet başına işçi, oturum başına kilit.
//
// SORUN
// Tetikleyici tek bir işçiyle çalışıyordu. "1 numaralı ödevi yap" gibi 12
// dakikalık bir iş başlayınca o süre boyunca gelen TÜM mesajlar — başka
// sohbetlerden gelenler dâhil — kuyrukta bekliyordu. Kullanıcının gördüğü
// tek şey sessizlikti.
//
// NEDEN AYRI İŞÇİ, NEDEN AYRI KİLİT
// İki kavram karıştırılmamalı:
//
//   · SOHBET → sıra korunmalı. Aynı konuşmaya iki cevap birbirine girerse
//     kullanıcı hangisinin neye cevap olduğunu ayıramaz. Aynı sohbetten
//     gelen mesajlar ASLA eşzamanlı işlenmez.
//
//   · OTURUM → aynı anda yalnız BİR yazıcı olmalı. opencode bir oturuma
//     paralel iki istek verilirse ikisinin konuşma geçmişi karışır ve cevaplar
//     birbirinin içine girer. Farklı sohbetler aynı oturumu paylaşıyorsa bu
//     kilit onları sıraya alır.
//
// İkisi farklı eksende olduğu için ikisi de gerekiyor: işçi SOHBET'e göre
// ayrılır (farklı sohbetler paralel çalışır), kilit OTURUM'a göredir (aynı
// oturuma paralel yazı olmaz).

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// isleyici, mesajları sohbet başına işçilere dağıtır.
//
// Garanti edilen şey mesajın SOHBET İÇİNDEKİ sırasıdır. Hangi sohbetin önce
// biteceği belirsizdir — bu bir hata değil, beklenen davranıştır.
type isleyici struct {
	calistir func(ctx context.Context, m message)
	derinlik int

	mu      sync.Mutex
	isciler map[string]*sohbetIscisi
	bekleme int
	kapandi bool

	bekleyen sync.WaitGroup
}

// sohbetIscisi, tek bir sohbetin işçisi ve kuyruğudur.
type sohbetIscisi struct {
	kanal chan message

	// son, işçinin en son ne zaman iş aldığı. Boşta kalan işçiler geri
	// çekilir (bosIsciSuresi).
	son time.Time
}

// isleyiciAyarlari, işleyicinin sınırlarını taşır.
type isleyiciAyarlari struct {
	// kanalDerinligi, sohbet başına bekleyebilecek mesaj sayısıdır.
	kanalDerinligi int

	// bosIsciSuresi, bu süre boyunca iş almayan işçi kapanır.
	bosIsciSuresi time.Duration

	// bosIsciTara, işçi temizliğinin aralığıdır.
	bosIsciTara time.Duration
}

// varsayilanIsleyiciAyarlari, tek kullanıcılı kurulum için makul değerler.
//
// Kanal derinliği 64: eski tek işçili kuyruğun derinliğiydi, daraltmadık ki
// davranış sessizce değişmesin. Boş işçi 5 dakika sonra kapanır — yeterince
// uzun ki bir sohbetin aralıkları (gün içinde birkaç mesaj) yeni işçi
// açtırmasın, yeterince kısa ki kullanılmayan sohbetler birikmesin.
func varsayilanIsleyiciAyarlari() isleyiciAyarlari {
	return isleyiciAyarlari{
		kanalDerinligi: 64,
		bosIsciSuresi:  5 * time.Minute,
		bosIsciTara:    time.Minute,
	}
}

// yeniIsleyici, mesajları işleyecek bir düzen kurar.
//
// calistir, tek bir mesajı işleyen işlevdir (engine.dispatch).
func yeniIsleyici(calistir func(ctx context.Context, m message), ayar isleyiciAyarlari) *isleyici {
	if ayar.kanalDerinligi < 1 {
		ayar.kanalDerinligi = 1
	}
	if ayar.bosIsciSuresi <= 0 {
		ayar.bosIsciSuresi = 5 * time.Minute
	}
	if ayar.bosIsciTara <= 0 {
		ayar.bosIsciTara = time.Minute
	}

	i := &isleyici{
		calistir: calistir,
		derinlik: ayar.kanalDerinligi,
		isciler:  make(map[string]*sohbetIscisi),
	}

	go i.bosIsciTemizle(ayar)
	return i
}

// besle, mesajı doğru sohbetin işçisine verir.
//
// Dönüş: false ise mesaj ALINMADI (düzen kapanmış veya akış kesildi).
// Çağıran bunu hata saymamalı; mesajın köprü tamponunda kalması doğru
// davranıştır — sessizce düşürmek kullanıcının mesajını kaybettirir.
func (i *isleyici) besle(ctx context.Context, m message) bool {
	i.mu.Lock()
	if i.kapandi {
		i.mu.Unlock()
		return false
	}
	isci := i.isciAl(m.From)
	i.bekleme++
	i.mu.Unlock()

	select {
	case isci.kanal <- m:
		return true
	case <-ctx.Done():
		// Akış kapandı hâlde kanal doluysa mesaj hâlâ bizde: sayacı düzelt.
		i.beklemeAzalt()
		return false
	}
}

// isciAl, sohbetin işçisini bulur; yoksa açar. i.mu kilidi altında çağrılır.
func (i *isleyici) isciAl(sohbet string) *sohbetIscisi {
	if isci, bulundu := i.isciler[sohbet]; bulundu {
		isci.son = time.Now()
		return isci
	}

	isci := &sohbetIscisi{
		kanal: make(chan message, i.derinlik),
		son:   time.Now(),
	}
	i.isciler[sohbet] = isci

	// Add, goroutine başlamadan ÖNCE yapılmalı: kapat() Wait'i bir işçi
	// başlatılana kadar beklemez, aksi hâlde işçi "kaçırılır" ve kapatma
	// bitmiş görünürken iş devam eder.
	i.bekleyen.Add(1)
	go i.calistirSohbet(sohbet, isci)
	logf("işçi açıldı: %s (%d sohbet açık)", kisaJID(sohbet), len(i.isciler))
	return isci
}

// calistirSohbet, tek bir sohbetin mesajlarını SIRAYLA işler.
func (i *isleyici) calistirSohbet(sohbet string, isci *sohbetIscisi) {
	defer i.bekleyen.Done()

	for m := range isci.kanal {
		i.mu.Lock()
		isci.son = time.Now()
		i.bekleme--
		i.mu.Unlock()

		i.calistir(context.Background(), m)
	}

	// Kanal kapandı. İşçi kaydı zaten kapatıcı tarafından silinmiş olabilir
	// (bkz. kapat); sadece logla.
	logf("işçi kapandı: %s", kisaJID(sohbet))
}

// bosIsciTemizle, mesaj almayan işçileri kapatır.
//
// Neden: bir sohbet bir kez yazdığında işçi sonsuza dek açık kalırdı. Yüzlerce
// sohbet görülmüş bir hesapta bu, kullanılmayan işçi ve kanal birikimidir.
func (i *isleyici) bosIsciTemizle(ayar isleyiciAyarlari) {
	ticker := time.NewTicker(ayar.bosIsciTara)
	defer ticker.Stop()

	for range ticker.C {
		i.bosIsciBirTemizle(ayar.bosIsciSuresi)
	}
}

// bosIsciBirTemizle, bir tur temizlik yapar ve kapatılan sayısını döndürür.
// Testten doğrudan çağrılabilir olsun diye gövdeden ayrıldı.
func (i *isleyici) bosIsciBirTemizle(bosSure time.Duration) int {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(i.isciler) == 0 {
		return 0
	}

	// Kanalda bekleyen mesaj varsa işçi boş değildir: kapatmak onları düşürür.
	var kapat []string
	for sohbet, isci := range i.isciler {
		if len(isci.kanal) == 0 && !isci.son.After(time.Now().Add(-bosSure)) {
			kapat = append(kapat, sohbet)
		}
	}

	for _, sohbet := range kapat {
		close(i.isciler[sohbet].kanal)
		delete(i.isciler, sohbet)
	}

	if len(kapat) > 0 {
		logf("%d boş işçi kapatıldı (%d sohbet açık)", len(kapat), len(i.isciler))
	}
	return len(kapat)
}

// kapat, düzeni durdurur: yeni mesaj alınmaz, açık kanallar kapatılır ve
// bekleyen işlerin bitmesi süreye kadar beklenir.
//
// Kapatmadan SONRA gelen mesajlar alınmaz; köprüde kalır ve yeniden
// bağlandığında catch-up taraması onları bulur.
func (i *isleyici) kapat(sure time.Duration) int {
	i.mu.Lock()
	if i.kapandi {
		i.mu.Unlock()
		return 0
	}
	i.kapandi = true

	isciSayisi := len(i.isciler)
	// Bekleyen sayısı BEKLEMEDEN ÖNCE okunur: bekleme bitince bu sayı zaten
	// sıfırdır ve çağıran "hiç iş beklemiyormuş" yanılgısına düşer.
	bekleyen := i.bekleme

	for sohbet, isci := range i.isciler {
		close(isci.kanal)
		delete(i.isciler, sohbet)
	}
	i.mu.Unlock()

	// Kanal kapanınca işçiler kuyruğu boşaltıp çıkar. bekleme (mesaj sayısı)
	// sıfır olabilir ama son mesaj HÂLÂ çalışıyor olabilir — sayaç iş
	// BAŞLAMADAN düşürüldüğü için. Doğru ölçüt işçi sayısıdır.
	if isciSayisi == 0 {
		return 0
	}

	logf("işleyici kapatılıyor: %d işçi bitiriliyor (%d bekleyen mesaj, en fazla %s)",
		isciSayisi, bekleyen, sure)

	bitti := make(chan struct{})
	go func() {
		i.bekleyen.Wait()
		close(bitti)
	}()

	sec := time.NewTimer(sure)
	defer sec.Stop()
	select {
	case <-bitti:
		logf("bekleyen işler bitti")
	case <-sec.C:
		if kalan := i.bekleyenSayisi(); kalan > 0 {
			logf("⚠️ kapatma süresi doldu, %d iş yarım kaldı", kalan)
		}
	}
	return bekleyen
}

// bekleyenSayisi, hâlâ işlenmemiş mesaj sayısını döndürür.
func (i *isleyici) bekleyenSayisi() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.bekleme
}

// beklemeAzalt, besle başarısız olduğunda sayacı düzeltir.
func (i *isleyici) beklemeAzalt() {
	i.mu.Lock()
	if i.bekleme > 0 {
		i.bekleme--
	}
	i.mu.Unlock()
}

// ozet, düzenin durumunu tek satırda anlatır (durum çıktısı için).
func (i *isleyici) ozet() string {
	i.mu.Lock()
	defer i.mu.Unlock()

	if len(i.isciler) == 0 {
		return "işleyici boş"
	}

	sohbetler := make([]string, 0, len(i.isciler))
	for sohbet, isci := range i.isciler {
		sohbetler = append(sohbetler, fmt.Sprintf("%s:%d", kisaJID(sohbet), len(isci.kanal)))
	}
	return fmt.Sprintf("%d sohbet açık · bekleyen %d · %s",
		len(i.isciler), i.bekleme, strings.Join(sohbetler, " "))
}

// kisaJID, günlük satırlarında okunabilir olsun diye sohbet kimliğini kısaltır.
//
// BAŞTAN korunur, sondan değil: LID'li sohbetlerin hepsi "@lid" ile biter,
// sondan kısaltmak hepsini aynı gösterirdi ("…48@lid"). Ayırt eden kısım
// baştır: "12345678901234@lid" → "123456…@lid".
//
// Kısaltma YALNIZCA günlük içindir — karşılaştırma, eşleştirme veya gönderimde
// asla kullanılmaz; orada tam kimlik gerekir (LID tuzağı).
func kisaJID(sohbet string) string {
	const gorunecek = 6
	if len(sohbet) <= gorunecek {
		return sohbet
	}
	return sohbet[:gorunecek] + "…"
}

// ── oturum kilidi ─────────────────────────────────────────────────────────

// oturumKilitle, aynı opencode oturumuna paralel yazmayı engeller.
//
// Anahtar oturum kimliğidir. Kilit bir kanal (kapasite 1) olarak modellenir,
// mutex değil: beklemek isteyen iş, bağlam iptal olunca beklemekten çıkabilmelidir
// — "kapat" komutu geldiğinde kilitte sıkışan bir iş sonsuza dek bekleyemez.
//
// Kapasitesi 1 olan kanal aynı zamanda mutex'tir, ama seçmeli (select) alınabilir.
// Bu yüzden kilitKazan gibi bir yardımcıya gerek yoktur.
var oturumKilitle sync.Map // sessionID → chan struct{}

// oturumKilidiAl, oturum kimliğine özel kilidi alır ve bırakma işlevini döndürür.
//
// Dönüş: bağlam iptal olduysa hata. Oturum kimliği boşsa KİLİT YOK döner:
// kilit almak "her eylemi aynı oturumda sıraya al" anlamına gelir ve tüm
// konuşmaları yine tek kuyruğa bağlardı (çözdüğümüz sorunun kendisi).
func oturumKilidiAl(ctx context.Context, oturum string) (func(), error) {
	oturum = strings.TrimSpace(oturum)
	if oturum == "" {
		return func() {}, nil
	}

	// LoadOrStore yarışında iki işçi aynı anda kanal yaratabilir; ikincisi
	// elde edilen kanalı kullanmalı, kendi yarattığını değil. Bu yüzden
	// LoadOrStore'ın DÖNDÜRDÜĞÜ değer kullanılır.
	var kanal chan struct{}
	if deger, varMi := oturumKilitle.Load(oturum); varMi {
		kanal = deger.(chan struct{})
	} else {
		yenilen, yuklendi := oturumKilitle.LoadOrStore(oturum, make(chan struct{}, 1))
		kanal = yenilen.(chan struct{})
		_ = yuklendi
	}

	select {
	case kanal <- struct{}{}:
		var birKez sync.Once
		return func() {
			// İki kez bırakılırsa ikinci çağrı paneli kilitli olmayan bir
			// kanala ekleme yapardı; Once bunu engeller.
			birKez.Do(func() { <-kanal })
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// oturumKilidiTest, yalnız testler için: kilit deposunu temizler.
//
// Neden gerekli: sync.Map genel birim testler arasında taşınırsa, diğer
// testlerin bırakmadığı bir kilit bu testin kilidi almasını bekletir ve test
// süresizce asılı kalır (Go test panik verir). Depoyu test başında sıfırlamak
// bu tür sızıntıyı görünmez kılar.
func oturumKilidiTest() {
	oturumKilitle.Range(func(anahtar, _ any) bool {
		oturumKilitle.Delete(anahtar)
		return true
	})
}
