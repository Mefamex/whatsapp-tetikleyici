// whatsapp-bridge-project/tetikleyici/reply.go

// /reply ADIM 2 (2026-10-06): ajan bir soru sorduysa bekleyen cevap dosyasına
// bir numara atar ve cevaba "> cevaplamak için /reply N" yönergesi ekler.
//
// Dosya biçimi Python cmd/reply.py ile ORTAK: `.tetikleyici/bekleyen_cevaplar.txt`,
// satır başına bir sayı (LF), chmod 600, tmp+rename ile atomik yazım.
// Numara kuralı (sahip tasarımı, TODO.md §/reply): en küçük eksik pozitif
// (1,2,4 → 3); dosya boşsa 0. Kullanıcı /reply N ile cevap verince Python
// tarafı numarayı dosyadan siler ve cevap ajan oturumuna taşınır (ilk ileti
// ADIM 2'nin ikinci yarısı olarak ayrı commit'e bırakıldı — bu dosya yalnız
// KAYIT yapar).
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// soruNotu, cevapta "?" varsa bir bekleyen cevap numarası atar ve metnin
// sonuna cevaplama yönergesi ekler. Soru yoksa ya da numara atanamazsa metin
// değişmeden döner — sistem "?" yüzünden cevap göndermeyi ASLA engellemez.
func soruNotu(configPath, reply string) (string, error) {
	metin := strings.TrimSpace(reply)
	if metin == "" || !strings.Contains(metin, "?") {
		return metin, nil
	}
	num, err := bekleyenNumaraAta(configPath)
	if err != nil {
		logf("  soru notu: bekleyen numara atanamadı: %v", err)
		return metin, nil
	}
	logf("  soru tespit edildi → bekleyen numara %d eklendi", num)
	return metin + "\n\n> cevaplamak için /reply veya /reply " + strconv.Itoa(num), nil
}

// bekleyenCevapYolu, bekleyen cevaplar dosyasının yerini söyler:
// BEKLEYEN_CEVAP_DOSYA env'i varsa o, yoksa config'in projesindeki
// .tetikleyici/bekleyen_cevaplar.txt (Python reply.py ile aynı dizin).
func bekleyenCevapYolu(configPath string) string {
	if d := strings.TrimSpace(os.Getenv("BEKLEYEN_CEVAP_DOSYA")); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(configPath), ".tetikleyici", "bekleyen_cevaplar.txt")
}

// bekleyenZamanAsimi, bekleyen cevap numarasının yaşam süresi. Bu süre
// içinde `/reply N` gelmezse numara serbest bırakılır (TODO §/reply: öneri
// 10 dk). Süre sabittir; uyarı yalnız günlüğe yazılır — kullanıcıya WhatsApp
// mesajı GİTMEZ, çünkü cevabı unutan kullanıcıyı hatırlatmak spam olur ve
// `/reply list` zaten günceli gösterir.
const bekleyenZamanAsimi = 10 * time.Minute

// bekleyenZamanYolu, bekleyen cevap numaralarının damga dosyasının yerini
// söyler: ana dosyanın yanında, ".zaman" eki ile. Biçim: satır başına
// "numara <epoch-sanіye>" (ASCII, LF) — Go ve Python ortak okur/yazar.
func bekleyenZamanYolu(configPath string) string {
	return bekleyenCevapYolu(configPath) + ".zaman"
}

// bekleyenDamgalar, damga dosyasındaki numara→epoch eşlemesini döndürür.
// Bozuk satırlar atlanır; dosya yoksa boş harita.
func bekleyenDamgalar(configPath string) map[int]int64 {
	veri, err := os.ReadFile(bekleyenZamanYolu(configPath))
	if err != nil {
		return map[int]int64{}
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

// bekleyenDamgaYaz, damga dosyasını numara→epoch haritasıyla atomik yazar.
func bekleyenDamgaYaz(configPath string, damgalar map[int]int64) error {
	yol := bekleyenZamanYolu(configPath)

	nums := make([]int, 0, len(damgalar))
	for n := range damgalar {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	var sb strings.Builder
	for i, n := range nums {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(strconv.Itoa(n))
		sb.WriteByte(' ')
		sb.WriteString(strconv.FormatInt(damgalar[n], 10))
	}
	if len(nums) > 0 {
		sb.WriteByte('\n')
	}

	if err := os.MkdirAll(filepath.Dir(yol), 0o700); err != nil {
		return err
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, yol); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// bekleyenTemizle, süresi dolmuş bekleyen cevap numaralarını hem ana hem
// damga dosyasından siler ve silinenleri döndürür. Damgası OLMAYAN
// numaralara dokunulmaz: yaşı bilinmeyeni silmek yanlış pozitif olur
// (eski format ya da elle eklenmiş kayıt).
func bekleyenTemizle(configPath string, simdi time.Time) []int {
	damgalar := bekleyenDamgalar(configPath)
	if len(damgalar) == 0 {
		return nil
	}

	esik := simdi.Add(-bekleyenZamanAsimi).Unix()
	bayat := []int{}
	for num, epoch := range damgalar {
		if epoch < esik {
			bayat = append(bayat, num)
		}
	}
	if len(bayat) == 0 {
		return nil
	}
	sort.Ints(bayat)

	bayatSet := map[int]bool{}
	for _, n := range bayat {
		bayatSet[n] = true
	}

	// Ana dosyadan bayatları çıkar.
	anaYol := bekleyenCevapYolu(configPath)
	guncel := make([]string, 0, len(bayatSet))
	if veri, err := os.ReadFile(anaYol); err == nil {
		for _, satir := range strings.Split(string(veri), "\n") {
			satir = strings.TrimSpace(satir)
			if n, err := strconv.Atoi(satir); err == nil && bayatSet[n] {
				continue
			}
			if satir != "" {
				guncel = append(guncel, satir)
			}
		}
	}
	metin := strings.Join(guncel, "\n")
	if len(guncel) > 0 {
		metin += "\n"
	}
	if err := os.WriteFile(anaYol+".tmp", []byte(metin), 0o600); err != nil {
		return nil
	}
	if err := os.Rename(anaYol+".tmp", anaYol); err != nil {
		os.Remove(anaYol + ".tmp")
		return nil
	}

	// Damga dosyasından bayatları çıkar.
	for _, n := range bayat {
		delete(damgalar, n)
	}
	if err := bekleyenDamgaYaz(configPath, damgalar); err != nil {
		logf("bekleyen zaman aşımı: damga dosyası yazılamadı: %v", err)
	}
	return bayat
}

// bekleyenSayilar, dosyadaki geçerli sayıları sıralı ve benzersiz döndürür.
// Bozuk satırlar atlanır; dosya yoksa boş liste.
func bekleyenSayilar(configPath string) []int {
	veri, err := os.ReadFile(bekleyenCevapYolu(configPath))
	if err != nil {
		return nil
	}
	gorulen := map[int]bool{}
	for _, satir := range strings.Split(string(veri), "\n") {
		satir = strings.TrimSpace(satir)
		if n, err := strconv.Atoi(satir); err == nil {
			gorulen[n] = true
		}
	}
	sayilar := make([]int, 0, len(gorulen))
	for n := range gorulen {
		sayilar = append(sayilar, n)
	}
	sort.Ints(sayilar)
	return sayilar
}

// enKucukEksik, Python reply.en_kucuk_eksik ile aynı kural:
// [] → 0 · [1,2,4] → 3 · [2,3] → 1 · [1,2,3,4] → 5.
func enKucukEksik(sayilar []int) int {
	if len(sayilar) == 0 {
		return 0
	}
	onceki := 0
	for _, s := range sayilar {
		if s-onceki > 1 {
			return onceki + 1
		}
		onceki = s
	}
	return onceki + 1
}

// bekleyenNumaraAta, en küçük eksik numarayı dosyaya atomik ekler ve döndürür.
// Önce süresi dolan bekleyenler serbest bırakılır (boşalan yer yeniden
// kullanılır); sonra numara hem ana hem damga dosyasına yazılır.
func bekleyenNumaraAta(configPath string) (int, error) {
	if bayat := bekleyenTemizle(configPath, time.Now()); len(bayat) > 0 {
		logf("  bekleyen cevap zaman aşımı: %v serbest bırakıldı", bayat)
	}

	yol := bekleyenCevapYolu(configPath)
	sayilar := bekleyenSayilar(configPath)
	num := enKucukEksik(sayilar)

	yeni := append(append([]int{}, sayilar...), num)
	sort.Ints(yeni)

	var sb strings.Builder
	for i, n := range yeni {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(strconv.Itoa(n))
	}
	if len(yeni) > 0 {
		sb.WriteByte('\n')
	}

	if err := os.MkdirAll(filepath.Dir(yol), 0o700); err != nil {
		return 0, err
	}
	tmp := yol + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, yol); err != nil {
		os.Remove(tmp)
		return 0, err
	}

	// Zaman damgası: numaranın bekleyişi şimdi başlar. Damga yazılamazsa
	// numara yine de atanmış sayılır (yönerge kullanıcıya gider); yalnız
	// zaman aşımına uğramaz — eski davranışa geri düşer.
	damgalar := bekleyenDamgalar(configPath)
	damgalar[num] = time.Now().Unix()
	if err := bekleyenDamgaYaz(configPath, damgalar); err != nil {
		logf("  bekleyen damga yazılamadı (%d): %v", num, err)
	}
	return num, nil
}
