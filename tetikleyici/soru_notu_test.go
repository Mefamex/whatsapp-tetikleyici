// whatsapp-bridge-project/tetikleyici/soru_notu_test.go
//
// /reply ADIM 2 — soruNotu + bekleyen cevap dosyası yönetimi testleri.
// (reply_test.go zaten çift gönderim/fakeBridge testleri barındırıyor;
// bu dosya yalnız yeni soru-notu mantığını test eder.)

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testTmpConfig, test için geçici bir config yolu döndürür. Config dosyasının
// var olması gerekmez — yalnızca dizini proje kökü yerine geçer.
func testTmpConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "tetikleyici.yaml")
}

func TestSoruNotu_SoruYoksaDegismez(t *testing.T) {
	got, err := soruNotu(testTmpConfig(t), "Bugün hava güzel, dışarı çıkabilirsin.")
	if err != nil {
		t.Fatalf("hata: %v", err)
	}
	if got != "Bugün hava güzel, dışarı çıkabilirsin." {
		t.Errorf("gereksiz değişiklik: %q", got)
	}
}

func TestSoruNotu_SoruVarsaNumaraVeYonergeEkler(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	dosya := filepath.Join(dir, "bekleyen_cevaplar.txt")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", dosya)

	got, err := soruNotu(cfg, "Fotoğrafı nasıl gönderebilirim?")
	if err != nil {
		t.Fatalf("hata: %v", err)
	}
	if !strings.Contains(got, "> cevaplamak için /reply veya /reply 0") {
		t.Errorf("yönerge eklenmedi: %q", got)
	}
	if sayilar := bekleyenSayilar(cfg); len(sayilar) != 1 || sayilar[0] != 0 {
		t.Errorf("bekleyenler = %v, want [0]", sayilar)
	}
}

func TestSoruNotu_ArdisikSorularNumaraBirerBirerArttirir(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	dosya := filepath.Join(dir, "bekleyen_cevaplar.txt")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", dosya)

	for _, beklenen := range []string{"0", "1", "2"} {
		got, _ := soruNotu(cfg, "Soru sorusu?")
		if !strings.Contains(got, "/reply "+beklenen) {
			t.Errorf("yönerge yanlış: %q (beklenen /reply %s)", got, beklenen)
		}
	}
}

func TestSoruNotu_KapiDoluncaEksikDoldurulur(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	dosya := filepath.Join(dir, "bekleyen_cevaplar.txt")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", dosya)

	soruNotu(cfg, "A?")
	soruNotu(cfg, "B?")
	soruNotu(cfg, "C?") // → 0,1,2
	// Python tarafı 1'i sildi (kullanıcı /reply 1 ile cevapladı) → 0,2
	if err := os.WriteFile(dosya, []byte("0\n2\n"), 0o600); err != nil {
		t.Fatalf("yazma: %v", err)
	}
	got, _ := soruNotu(cfg, "D?")
	if !strings.Contains(got, "/reply 1") {
		t.Errorf("eksik 1 doldurulmalıydı: %q", got)
	}
}

func TestBekleyenSayilar_BozukSatirlarAtlanir(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	dosya := filepath.Join(dir, "bekleyen_cevaplar.txt")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", dosya)

	if err := os.WriteFile(dosya, []byte("2\nbozuk\n1\n\n4\n"), 0o600); err != nil {
		t.Fatalf("yazma: %v", err)
	}
	got := bekleyenSayilar(cfg)
	want := []int{1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("bekleyenler = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bekleyenler = %v, want %v", got, want)
		}
	}
}

func TestEnKucukEksik(t *testing.T) {
	kontrol := []struct {
		veri []int
		want int
	}{
		{nil, 0},
		{[]int{}, 0},
		{[]int{0}, 1},
		{[]int{1, 2, 4}, 3},
		{[]int{2, 3}, 1},
		{[]int{1, 2, 3, 4}, 5},
		{[]int{0, 2, 3}, 1},
	}
	for _, k := range kontrol {
		if got := enKucukEksik(k.veri); got != k.want {
			t.Errorf("enKucukEksik(%v) = %d, want %d", k.veri, got, k.want)
		}
	}
}

func TestBekleyenNumaraAta_AtomikVeChmod(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "tetikleyici.yaml")
	dosya := filepath.Join(dir, "alt", "bekleyen_cevaplar.txt")
	t.Setenv("BEKLEYEN_CEVAP_DOSYA", dosya)

	num, err := bekleyenNumaraAta(cfg)
	if err != nil {
		t.Fatalf("ata: %v", err)
	}
	if num != 0 {
		t.Errorf("ilk numara = %d, want 0", num)
	}
	st, err := os.Stat(dosya)
	if err != nil {
		t.Fatalf("dosya yok: %v", err)
	}
	if mod := st.Mode().Perm(); mod != 0o600 {
		t.Errorf("chmod = %o, want 600", mod)
	}
	if _, err := os.Stat(dosya + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp artığı kalmamalı (atomik yazım)")
	}
}
