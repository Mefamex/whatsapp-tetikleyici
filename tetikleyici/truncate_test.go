package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Günlük satırında bozuk UTF-8 satırı günlük ayrıştırıcılarını bozuyordu:
// 200 baytta kesme çok baytlı karakteri ortasından bölüyordu.
// Ölçülen örnek: "administrator olarak de�" (export/1-mefamex, 21:09).
func TestTruncateCokBaytliKarakteriBolmez(t *testing.T) {
	// 4 baytlık karakterlerden (emoji) oluşan, 200 bayt sınırına denk gelen metin.
	var b strings.Builder
	for b.Len() < 260 {
		b.WriteString("de� ")
	}
	girdi := b.String()

	cikti := truncate(girdi, 200)

	if !utf8.ValidString(cikti) {
		t.Errorf("çıktı geçerli UTF-8 değil: %q", cikti[len(cikti)-6:])
	}
	if !strings.HasSuffix(cikti, "...") {
		t.Error("kısaltma işareti yok")
	}
	if len(cikti) > 200 {
		t.Errorf("çıktı sınırı aştı: %d bayt", len(cikti))
	}
}

func TestTruncateKisaMetneDokunmaz(t *testing.T) {
	if got := truncate("kısa mesaj", 200); got != "kısa mesaj" {
		t.Errorf("kısa metin değişti: %q", got)
	}
}

func TestTruncateSatirSonlariniDuzleştirir(t *testing.T) {
	if got := truncate("  birinci\nikinci  ", 200); got != "birinci ikinci" {
		t.Errorf("satır sonları düzleşmedi: %q", got)
	}
}

func TestTruncateCokKucukSinirPatlamaz(t *testing.T) {
	if got := truncate("abcdef", 2); got != "ab" {
		t.Errorf("max=2: %q", got)
	}
}
