// whatsapp-bridge-project/tetikleyici/bekleyen_zaman_test.go
//
// Zaman aşımı: süresi dolan bekleyen cevap numaraları serbest bırakılır.
// (TODO §/reply: "bekleyen cevap 10 dk gelmezse ne olur? → numara serbest,
// uyarı günlüğe"). Damga dosyası `<ana>.zaman` biçimindedir ve Python
// cmd/reply.py ile ORTAK okunur/yazılır.

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// bekleyenHazir, temp dizinde bekleyen cevap dosyaları olan bir ortam kurar
// ve config yolunu döndürür. Ana dosya varsayılan yolun yerine env ile
// temp dizine yönlendirilir; damga dosyası otomatik olarak yanında olur.
func bekleyenHazir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", filepath.Join(dir, "bekleyen_cevaplar.txt"))
	return cfg
}

// bekleyenZamanOku, testler için damga dosyasını "numara epoch" haritası
// olarak okur.
func bekleyenZamanOku(t *testing.T, cfg string) map[int]int64 {
	t.Helper()
	veri, err := os.ReadFile(bekleyenZamanYolu(cfg))
	if err != nil {
		t.Fatalf("damga okunamadı: %v", err)
	}
	damgalar := map[int]int64{}
	for _, satir := range strings.Split(string(veri), "\n") {
		parca := strings.Fields(satir)
		if len(parca) != 2 {
			continue
		}
		num, err1 := strconv.Atoi(parca[0])
		epoch, err2 := strconv.ParseInt(parca[1], 10, 64)
		if err1 == nil && err2 == nil {
			damgalar[num] = epoch
		}
	}
	return damgalar
}

// TestBekleyenTemizle_BayatSerbestBirakilir: 10 dk'yı aşan damga hem ana
// hem damga dosyasından silinir, genç numara kalır.
func TestBekleyenTemizle_BayatSerbestBirakilir(t *testing.T) {
	cfg := bekleyenHazir(t)
	simdi := time.Now()

	txt := bekleyenCevapYolu(cfg)
	if err := os.WriteFile(txt, []byte("1\n2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	damgalar := map[int]int64{1: simdi.Add(-bekleyenZamanAsimi - time.Minute).Unix(), 2: simdi.Unix()}
	if err := bekleyenDamgaYaz(cfg, damgalar); err != nil {
		t.Fatal(err)
	}

	silinen := bekleyenTemizle(cfg, simdi)

	if len(silinen) != 1 || silinen[0] != 1 {
		t.Errorf("silinen = %v, want [1]", silinen)
	}
	if got := bekleyenSayilar(cfg); len(got) != 1 || got[0] != 2 {
		t.Errorf("kalan bekleyenler = %v, want [2]", got)
	}
	sonra := bekleyenZamanOku(t, cfg)
	if len(sonra) != 1 || sonra[2] != damgalar[2] {
		t.Errorf("kalan damgalar = %v, want yalnız 2", sonra)
	}
}

// TestBekleyenTemizle_GenclereDokunulmaz: tüm damgalar süre içindeyse hiçbir
// şey silinmez.
func TestBekleyenTemizle_GenclereDokunulmaz(t *testing.T) {
	cfg := bekleyenHazir(t)
	simdi := time.Now()

	txt := bekleyenCevapYolu(cfg)
	if err := os.WriteFile(txt, []byte("0\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	damgalar := map[int]int64{0: simdi.Unix(), 1: simdi.Add(-time.Minute).Unix()}
	if err := bekleyenDamgaYaz(cfg, damgalar); err != nil {
		t.Fatal(err)
	}

	if got := bekleyenTemizle(cfg, simdi); got != nil {
		t.Errorf("genç numaralar silinmemeli: %v", got)
	}
	if got := bekleyenSayilar(cfg); len(got) != 2 {
		t.Errorf("bekleyenler değişti: %v", got)
	}
}

// TestBekleyenTemizle_DamgasizNumarayaDokunulmaz: damgası olmayan (eski
// format / elle eklenmiş) numara asla silinmez — yaşı bilinmiyor.
func TestBekleyenTemizle_DamgasizNumarayaDokunulmaz(t *testing.T) {
	cfg := bekleyenHazir(t)

	txt := bekleyenCevapYolu(cfg)
	if err := os.WriteFile(txt, []byte("7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Damga dosyası yok — 7'nin yaşı bilinmiyor.

	if got := bekleyenTemizle(cfg, time.Now()); got != nil {
		t.Errorf("damgasız numara silinmemeli: %v", got)
	}
	if got := bekleyenSayilar(cfg); len(got) != 1 || got[0] != 7 {
		t.Errorf("bekleyenler = %v, want [7]", got)
	}
}

// TestBekleyenNumaraAta_DamgaYazar: numara atanınca damga dosyasında da
// kaydı oluşur (bekleyişin başlangıcı).
func TestBekleyenNumaraAta_DamgaYazar(t *testing.T) {
	cfg := bekleyenHazir(t)

	num, err := bekleyenNumaraAta(cfg)
	if err != nil {
		t.Fatalf("ata: %v", err)
	}
	damgalar := bekleyenZamanOku(t, cfg)
	epoch, varMi := damgalar[num]
	if !varMi {
		t.Fatal("atanan numaranın damgası yok")
	}
	simdi := time.Now().Unix()
	if epoch < simdi-5 || epoch > simdi+5 {
		t.Errorf("damga şimdiki zaman civarında olmalı: %d", epoch)
	}
}

// TestSoruNotu_BayatiSerbestBirakir: süresi dolan numara bir sonraki soruda
// temizlenir ve yenisi, kalan en küçük boşluğa atanır. (Bayat 0 temizlenir;
// 1 duruyorken boşluk 2'dir — enKucukEksik kuralı.)
func TestSoruNotu_BayatiSerbestBirakir(t *testing.T) {
	cfg := bekleyenHazir(t)
	simdi := time.Now()

	txt := bekleyenCevapYolu(cfg)
	if err := os.WriteFile(txt, []byte("0\n1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	damgalar := map[int]int64{0: simdi.Add(-bekleyenZamanAsimi - time.Hour).Unix(), 1: simdi.Unix()}
	if err := bekleyenDamgaYaz(cfg, damgalar); err != nil {
		t.Fatal(err)
	}

	got, err := soruNotu(cfg, "Nasıl gidebilirim?")
	if err != nil {
		t.Fatalf("soruNotu: %v", err)
	}
	if !strings.Contains(got, "/reply 2") {
		t.Errorf("bayat 0 temizlenip 2 atanmalı: %q", got)
	}
	// Bayat 0 yok; 1 (genç) duruyor; yeni 2 eklendi.
	if sayilar := bekleyenSayilar(cfg); len(sayilar) != 2 || sayilar[0] != 1 || sayilar[1] != 2 {
		t.Errorf("bekleyenler = %v, want [1 2]", sayilar)
	}
}

// TestBekleyenZamanYolu_AnaDosyaninYaninda: damga dosyası ana dosyanın
// yanında ve adı "<ana>.zaman".
func TestBekleyenZamanYolu_AnaDosyaninYaninda(t *testing.T) {
	cfg := bekleyenHazir(t)
	if zaman := bekleyenZamanYolu(cfg); zaman != bekleyenCevapYolu(cfg)+".zaman" {
		t.Errorf("damga yolu = %q, want %q", zaman, bekleyenCevapYolu(cfg)+".zaman")
	}
}
