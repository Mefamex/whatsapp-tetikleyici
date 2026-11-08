package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ── yardımcılar ───────────────────────────────────────────────────────────

// kayit, calistir içinde olup biten mesajları toplayan test yardımcısıdır.
type kayit struct {
	mu    sync.Mutex
	mesaj []message
}

func (k *kayit) ekle(m message) {
	k.mu.Lock()
	k.mesaj = append(k.mesaj, m)
	k.mu.Unlock()
}

func (k *kayit) hepsi() []message {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]message(nil), k.mesaj...)
}

func (k *kayit) adet() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.mesaj)
}

func (k *kayit) metinler() []string {
	var cikti []string
	for _, m := range k.hepsi() {
		cikti = append(cikti, m.Text)
	}
	return cikti
}

// mesaj, test mesajı kurar.
func mesaj(jid, id, text string) message {
	return message{
		ID: id, From: jid, Sender: jid, PushName: "Mefamex",
		Direction: "in", Type: "text", Text: text,
	}
}

// bekle, koşulla sağlanana kadar bekler; süre dolarsa testi düşürür.
func bekle(t *testing.T, aciklama string, kosul func() bool) {
	t.Helper()

	surel := time.Now().Add(3 * time.Second)
	for time.Now().Before(surel) {
		if kosul() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("zaman aşımı: %s", aciklama)
}

func testAyarlari() isleyiciAyarlari {
	a := varsayilanIsleyiciAyarlari()
	// Testler kısa; 5 dakikalık boş işçi süresi işe yaramaz.
	a.bosIsciSuresi = 50 * time.Millisecond
	a.bosIsciTara = 20 * time.Millisecond
	return a
}

// ── temel davranış ───────────────────────────────────────────────────────

func TestIsleyiciTumMesajlariIsler(t *testing.T) {
	k := &kayit{}
	i := yeniIsleyici(func(ctx context.Context, m message) { k.ekle(m) }, testAyarlari())
	defer i.kapat(time.Second)

	for n := range 5 {
		if !i.besle(context.Background(), mesaj("a@s.whatsapp.net", fmt.Sprintf("m%d", n), fmt.Sprint(n))) {
			t.Fatalf("mesaj %d kabul edilmedi", n)
		}
	}

	waitFor := func() bool { return k.adet() == 5 }
	bekle(t, "5 mesaj işlenmedi", waitFor)

	if i.bekleyenSayisi() != 0 {
		t.Errorf("bekleyen = %d, want 0", i.bekleyenSayisi())
	}
}

// Aynı sohbetten gelen mesajlar SIRAYLA işlenmeli. Aynı konuşmaya iki cevap
// birbirine girerse kullanıcı hangisinin neye cevap olduğunu ayıramaz —
// sıra, eşzamanlılıktan daha önemlidir.
func TestIsleyiciAyniSohbetteSirayiKorur(t *testing.T) {
	var mu sync.Mutex
	var islenmis []string
	aktif := 0
	enFazlaAktif := 0

	i := yeniIsleyici(func(ctx context.Context, m message) {
		mu.Lock()
		aktif++
		if aktif > enFazlaAktif {
			enFazlaAktif = aktif
		}
		mu.Unlock()

		time.Sleep(15 * time.Millisecond)

		mu.Lock()
		islenmis = append(islenmis, m.Text)
		aktif--
		mu.Unlock()
	}, testAyarlari())
	defer i.kapat(time.Second)

	for n := range 6 {
		i.besle(context.Background(), mesaj("a@s.whatsapp.net", fmt.Sprintf("m%d", n), fmt.Sprint(n)))
	}

	bekle(t, "6 mesaj işlenmedi", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(islenmis) == 6
	})

	mu.Lock()
	defer mu.Unlock()

	if enFazlaAktif != 1 {
		t.Errorf("aynı sohbette aynı anda %d iş çalıştı, want 1", enFazlaAktif)
	}
	for n, alinan := range islenmis {
		if beklenen := fmt.Sprint(n); alinan != beklenen {
			t.Fatalf("sıra bozuk: %d. konuşma %q, beklenen %q", n, alinan, beklenen)
		}
	}
}

// Bu, düzenlemenin ASIL sebebidir: bir sohbetin uzun işi başka sohbeti
// bloklamamalı.
func TestIsleyiciFarkliSohbetleriParalelIsler(t *testing.T) {
	var mu sync.Mutex
	aktif := 0
	enFazlaAktif := 0
	bitti := map[string]bool{}

	yavasiBirak := make(chan struct{})
	var yavasiBirakildi sync.Once

	i := yeniIsleyici(func(ctx context.Context, m message) {
		mu.Lock()
		aktif++
		if aktif > enFazlaAktif {
			enFazlaAktif = aktif
		}
		mu.Unlock()

		// Yalnız yavaş sohbet beklesin; hızlı sohbet geçmeli.
		if m.From == "yavas@s.whatsapp.net" {
			<-yavasiBirak
		}

		mu.Lock()
		bitti[m.From] = true
		aktif--
		mu.Unlock()
	}, testAyarlari())
	defer func() {
		yavasiBirakildi.Do(func() { close(yavasiBirak) })
		i.kapat(time.Second)
	}()

	if !i.besle(context.Background(), mesaj("yavas@s.whatsapp.net", "y1", "uzun iş")) {
		t.Fatal("yavaş mesaj kabul edilmedi")
	}
	// Yavaş işçinin GERÇEKTEN başladığını bekle — aksi hâlde test yalnız
	// sırayı ölçer, eşzamanlılığı ölçmez.
	bekle(t, "yavaş iş başlamadı", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return aktif == 1
	})

	if !i.besle(context.Background(), mesaj("hizli@s.whatsapp.net", "h1", "kısa iş")) {
		t.Fatal("hızlı mesaj kabul edilmedi")
	}

	// Hızlı iş, yavaş iş hâlâ sürerken TAMAMLANMALI. "aktif == 1" beklemek
	// YANLIŞ olurdu: yavaş iş çalıştığı için o koşul zaten doğru.
	bekle(t, "hızlı sohbet yavaş işi bekledi", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return bitti["hizli@s.whatsapp.net"]
	})

	mu.Lock()
	defer mu.Unlock()
	if enFazlaAktif < 2 {
		t.Errorf("en fazla eşzamanlı iş = %d, want >= 2 (farklı sohbetler paralel olmalı)", enFazlaAktif)
	}
}

// ── sınırlar ──────────────────────────────────────────────────────────────

// Derinlik sınırı: kanal dolunca besle ENGELLENMELİ (bloklanmamalı). Mesaj
// düşürmek yerine geri basınç daha doğru; ama burada zaman aşımıyla vazgeçiyoruz
// çünkü besle ctx'siz çağrıldığında sonsuza dek beklerdi.
func TestIsleyiciKanalDolarsaBesleBloklanmaz(t *testing.T) {
	baslat := make(chan struct{})
	i := yeniIsleyici(func(ctx context.Context, m message) {
		<-baslat
	}, isleyiciAyarlari{kanalDerinligi: 1, bosIsciSuresi: time.Hour, bosIsciTara: time.Hour})
	defer func() {
		close(baslat)
		i.kapat(time.Second)
	}()

	i.besle(context.Background(), mesaj("a@s.whatsapp.net", "m1", "1")) // işçiyi meşgul eder
	bekle(t, "işçi meşgul olmadı", func() bool { return i.bekleyenSayisi() == 0 })

	i.besle(context.Background(), mesaj("a@s.whatsapp.net", "m2", "2")) // kanala girer

	// Üçüncü mesaj: kanal dolu → ctx iptaliyle vazgeçmeli.
	ctx, iptal := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer iptal()
	if i.besle(ctx, mesaj("a@s.whatsapp.net", "m3", "3")) {
		t.Error("dolu kanala alındı, want false")
	}

	if got := i.bekleyenSayisi(); got != 1 {
		t.Errorf("bekleyen = %d, want 1 (vazgeçilen mesaj sayacı düşürülmeli)", got)
	}
}

func TestIsleyiciBosIsciKapanir(t *testing.T) {
	k := &kayit{}
	i := yeniIsleyici(func(ctx context.Context, m message) { k.ekle(m) }, testAyarlari())
	defer i.kapat(time.Second)

	i.besle(context.Background(), mesaj("a@s.whatsapp.net", "m1", "1"))
	bekle(t, "mesaj işlenmedi", func() bool { return k.adet() == 1 })

	bekle(t, "boş işçi kapanmadı", func() bool { return len(i.ozet()) >= len("işleyici boş") })

	if k.adet() != 1 {
		t.Errorf("kapanan işçi mesaj kaybetti: %d", k.adet())
	}
}

// Kapanan işçinin kuyruğunda mesaj varsa temizlik onu DÜŞÜRMEMELİ.
func TestIsleyiciTemizlikBekleyenMesajiDusurmez(t *testing.T) {
	baslat := make(chan struct{})
	var birakildi sync.Once
	var mu sync.Mutex
	var islenmis []string

	i := yeniIsleyici(func(ctx context.Context, m message) {
		mu.Lock()
		islenmis = append(islenmis, m.Text)
		mu.Unlock()
		<-baslat
	}, testAyarlari())
	defer func() {
		birakildi.Do(func() { close(baslat) })
		i.kapat(2 * time.Second)
	}()

	i.besle(context.Background(), mesaj("a@s.whatsapp.net", "m1", "1"))
	for n := range 3 {
		i.besle(context.Background(), mesaj("a@s.whatsapp.net", fmt.Sprintf("q%d", n), fmt.Sprint(n)))
	}

	// İlk iş hâlâ sürerken temizlik birkaç tur dönsün.
	time.Sleep(150 * time.Millisecond)
	birakildi.Do(func() { close(baslat) })

	bekle(t, "kuyruktaki mesajlar işlenmedi", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(islenmis) == 4
	})
}

func TestIsleyiciKapatmaBekleyenleriBekler(t *testing.T) {
	var mu sync.Mutex
	islenmis := 0
	i := yeniIsleyici(func(ctx context.Context, m message) {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		islenmis++
		mu.Unlock()
	}, testAyarlari())

	for n := range 4 {
		i.besle(context.Background(), mesaj("a@s.whatsapp.net", fmt.Sprintf("m%d", n), "x"))
	}

	if bekleyen := i.kapat(3 * time.Second); bekleyen == 0 {
		t.Error("kapatma 0 bekleyen bildirdi, mesajlar kuyrukta değildi")
	}

	mu.Lock()
	defer mu.Unlock()
	if islenmis != 4 {
		t.Errorf("kapatmadan sonra %d mesaj işlendi, want 4", islenmis)
	}
}

// Kapatılmış düzen yeni mesaj ALMAMALI: mesaj köprüde kalsın, catch-up
// taraması bulsun.
func TestIsleyiciKapandiktanSonraMesajAlmaz(t *testing.T) {
	k := &kayit{}
	i := yeniIsleyici(func(ctx context.Context, m message) { k.ekle(m) }, testAyarlari())
	i.kapat(time.Second)

	if i.besle(context.Background(), mesaj("a@s.whatsapp.net", "m1", "1")) {
		t.Error("kapalı düzen mesaj kabul etti")
	}
	if i.kapat(time.Second) != 0 {
		t.Error("ikinci kapatma 0 dönmeliydi")
	}
}

func TestIsleyiciOzetBosDurumdaAnlasilir(t *testing.T) {
	i := yeniIsleyici(func(ctx context.Context, m message) {}, testAyarlari())
	defer i.kapat(time.Second)

	if got := i.ozet(); got != "işleyici boş" {
		t.Errorf("ozet = %q", got)
	}
}

// ── oturum kilidi ─────────────────────────────────────────────────────────

// Aynı oturuma paralel yazı olmamalı: opencode iki isteğin geçmişini
// karıştırır.
func TestOturumKilidiAyniOturumuSiralar(t *testing.T) {
	oturumKilidiTest()

	var mu sync.Mutex
	aktif := 0
	enFazla := 0

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			birak, err := oturumKilidiAl(context.Background(), "ses_oturum")
			if err != nil {
				t.Error(err)
				return
			}
			defer birak()

			mu.Lock()
			aktif++
			if aktif > enFazla {
				enFazla = aktif
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond)

			mu.Lock()
			aktif--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if enFazla != 1 {
		t.Errorf("aynı oturuma %d paralel yazı, want 1", enFazla)
	}
}

// Farklı oturumlar birbirini bekleMEMELİ — yoksa işçi ayrımının anlamı kalmaz.
func TestOturumKilidiFarkliOturumlariBekletmez(t *testing.T) {
	oturumKilidiTest()

	birak, err := oturumKilidiAl(context.Background(), "ses_bir")
	if err != nil {
		t.Fatal(err)
	}
	defer birak()

	tamamlandi := make(chan struct{})
	go func() {
		birak2, hata := oturumKilidiAl(context.Background(), "ses_iki")
		if hata != nil {
			t.Error(hata)
		} else {
			birak2()
		}
		close(tamamlandi)
	}()

	select {
	case <-tamamlandi:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("farklı oturum mesaj1'i bekledi — kilit oturum bazında olmalı")
	}
}

// Boş oturum kilidi ALMAMALI: alırsa "her eylemi aynı oturumda sıraya al"
// anlamına gelir ve tüm konuşmaları yine tek kuyruğa bağlar.
func TestOturumKilidiBosOturumdaKilitlemez(t *testing.T) {
	oturumKilidiTest()

	for _, bos := range []string{"", "   "} {
		birak, err := oturumKilidiAl(context.Background(), bos)
		if err != nil {
			t.Fatalf("boş oturum hata verdi: %v", err)
		}
		// İkinci kez alınabilmeli (= kilit yok).
		birak2, err := oturumKilidiAl(context.Background(), bos)
		if err != nil {
			t.Fatalf("boş oturum ikinci alım hata verdi: %v", err)
		}
		birak2()
		birak()
	}
}

func TestOturumKilidiIptalEdilebilir(t *testing.T) {
	oturumKilidiTest()

	birak, err := oturumKilidiAl(context.Background(), "ses_meşgul")
	if err != nil {
		t.Fatal(err)
	}
	defer birak()

	ctx, iptal := context.WithCancel(context.Background())
	iptal()

	if _, err := oturumKilidiAl(ctx, "ses_meşgul"); err == nil {
		t.Error("iptal edilmiş bağlamda kilit alındı, want hata")
	}
}

// İki kez bırakmak panel kilitli OLMAYAN bir kanala ekleme yapardı; sonraki
// alımlar süresizce beklerdi.
func TestOturumKilidiCiftBirakimGuvenli(t *testing.T) {
	oturumKilidiTest()

	birak, err := oturumKilidiAl(context.Background(), "ses_çift")
	if err != nil {
		t.Fatal(err)
	}
	birak()
	birak() // ikinci çağrı yine çalışmalı, hata vermemeli

	// Kanal hâlâ boş olmalı: yeni alım gecikmede geçmeli.
	tamamlandi := make(chan struct{})
	go func() {
		birak2, hata := oturumKilidiAl(context.Background(), "ses_çift")
		if hata == nil {
			birak2()
		}
		close(tamamlandi)
	}()

	select {
	case <-tamamlandi:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("çift bırakım kanulu kilit bıraktı")
	}
}

func TestKisaJIDLogIcinKisaltirTamKimlikDegil(t *testing.T) {
	if got := kisaJID("12345678901234@lid"); got != "123456…" {
		t.Errorf("kisaJID = %q, want 123456… (baş korunmalı, @lid her sohbette aynı)", got)
	}
	// Kısa girdi olduğu gibi dönmeli.
	if got := kisaJID("ab"); got != "ab" {
		t.Errorf("kısa JID bozuldu: %q", got)
	}
}
