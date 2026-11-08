package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// zamanlanmisTestIş, testler için hızlı bir iş üretir. FireAt geçmişse
// "vadesi gelmiş" demektir.
func zamanlanmisTestIş(id int64, fireAt int64) zamanlanmisJob {
	return zamanlanmisJob{
		ID:      id,
		To:      "12345678901234@lid",
		Text:    "zamanlanmış test mesajı",
		FireAt:  fireAt,
		Created: fireAt - 60,
	}
}

// TestZamanlanmisVadesiGelenGonderilir: vadesi gelen iş gönderilir ve dosya
// silinir; vadesi gelmeyen işe dokunulmaz.
func TestZamanlanmisVadesiGelenGonderilir(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)
	dizin := t.TempDir()

	simdi := time.Now().Unix()
	gelen := zamanlanmisTestIş(1, simdi-10)     // vadesi geçmiş
	bekleyen := zamanlanmisTestIş(2, simdi+999) // ileride
	if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, gelen.ID), gelen); err != nil {
		t.Fatal(err)
	}
	if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, bekleyen.ID), bekleyen); err != nil {
		t.Fatal(err)
	}

	zamanlanmisTur(context.Background(), client, dizin)

	if len(fake.sent) != 1 {
		t.Fatalf("beklenen 1 gönderim, gelen %d", len(fake.sent))
	}
	if fake.sent[0].To != gelen.To || fake.sent[0].Text != gelen.Text {
		t.Fatalf("yanlış gönderim: %+v", fake.sent[0])
	}
	if kalan := zamanlanmisOku(dizin); len(kalan) != 1 || kalan[0].ID != bekleyen.ID {
		t.Fatalf("bekleyen iş kayboldu, kalan: %+v", kalan)
	}
}

// TestZamanlanmisHicIshDokunmaz: köprüsüz kipte hiçbir iş gönderilmez, silinmez.
func TestZamanlanmisHicIshDokunmaz(t *testing.T) {
	dizin := t.TempDir()
	simdi := time.Now().Unix()
	if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, 7), zamanlanmisTestIş(7, simdi-5)); err != nil {
		t.Fatal(err)
	}

	zamanlanmisTur(context.Background(), nil, dizin)

	if kalan := zamanlanmisOku(dizin); len(kalan) != 1 {
		t.Fatalf("köprüsüz kip işi silmemeli, kalan: %+v", kalan)
	}
}

// TestZamanlanmisDenemeVeIptal: gönderim başarısızsa iş silinmez, deneme sayar;
// azami denemede iptal edilir.
func TestZamanlanmisDenemeVeIptal(t *testing.T) {
	fake := &fakeBridge{failWith: http.StatusInternalServerError}
	client := fake.start(t)
	dizin := t.TempDir()

	simdi := time.Now().Unix()
	is := zamanlanmisTestIş(9, simdi-10)
	if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, is.ID), is); err != nil {
		t.Fatal(err)
	}

	// Denemeler arası nefes 1 dakika — her tur arasında SonDeneme'yi geriye
	// çekerek nefes kuralını aşarız (test, gerçek zamanı beklemez).
	zamanlanmisTur(context.Background(), client, dizin)
	if kalan := zamanlanmisOku(dizin); len(kalan) != 1 || kalan[0].Deneme != 1 {
		t.Fatalf("1. deneme sonrası beklenen Deneme=1, gelen: %+v", kalan)
	}

	for deneme := 2; deneme <= zamanlanmisMaxDeneme; deneme++ {
		isler := zamanlanmisOku(dizin)
		if len(isler) != 1 {
			t.Fatalf("deneme %d öncesi iş kayboldu", deneme)
		}
		isler[0].SonDeneme = 0 // nefesi sıfırla
		if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, isler[0].ID), isler[0]); err != nil {
			t.Fatal(err)
		}
		zamanlanmisTur(context.Background(), client, dizin)
	}

	if kalan := zamanlanmisOku(dizin); len(kalan) != 0 {
		t.Fatalf("azami deneme sonrası iş iptal edilmeli, kalan: %+v", kalan)
	}
}

// TestZamanlanmisNefes: başarısız denemeden 1 dakika geçmeden yeniden denenmez.
func TestZamanlanmisNefes(t *testing.T) {
	fake := &fakeBridge{failWith: http.StatusInternalServerError}
	client := fake.start(t)
	dizin := t.TempDir()

	simdi := time.Now().Unix()
	is := zamanlanmisTestIş(11, simdi-10)
	is.Deneme = 1
	is.SonDeneme = simdi // biraz önce denendi
	if err := zamanlanmisKaydet(zamanlanmisJobYol(dizin, is.ID), is); err != nil {
		t.Fatal(err)
	}

	zamanlanmisTur(context.Background(), client, dizin)

	if kalan := zamanlanmisOku(dizin); len(kalan) != 1 || kalan[0].Deneme != 1 {
		t.Fatalf("nefes dolmadan deneme yapılmamalı, kalan: %+v", kalan)
	}
	if len(fake.sent) != 0 {
		t.Fatalf("nefes dolmadan gönderim denenmemeli")
	}
}

// TestZamanlanmisAcikVarsayilanKapali: yapılandırılmamış bloğun varsayılanı
// kapalıdır (MEVZU); açık yalnızca acik: true ile.
func TestZamanlanmisAcikVarsayilanKapali(t *testing.T) {
	var z zamanlanmisConfig
	if z.enabled() {
		t.Fatal("boş config kapalı olmalı")
	}
	z.Acik = &testTrue
	if !z.enabled() {
		t.Fatal("acik: true açık olmalı")
	}
	z.Acik = &testFalse
	if z.enabled() {
		t.Fatal("acik: false kapalı olmalı")
	}
}

var (
	testTrue  = true
	testFalse = false
)
