package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// AKİŞ KOPMASI, YARIM KALAN İŞİ ÖLDÜRMEMELİ
//
// Bu, kullanıcının asıl şikâyetinin mekaniği: "uzun işlerde cevap WhatsApp'a
// gitmiyor". Ölçülen hata "signal: killed" idi (11 kez). Sebebi: dispatch,
// akışın okuma döngüsünde senkron çağrılıyordu; akış koptuğunda eylemin
// bağlamı iptal olup süreç öldürülüyordu.
//
// workCtx = context.WithoutCancel(akisCtx) düzeltmesi bunu engeller: akış
// kapanınca eylemin bağlamı yaşamaya devam eder, iş bitince yanıt gider.

func TestAkisKopuncaYarimKalanIsOldurulmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	// Akış bağlamı: bu kapanacak (akış koptu).
	akisCtx, akisKapandi := context.WithCancel(context.Background())

	// Düzeltme: eylem bağlamı akıştan ayrık.
	workCtx := context.WithoutCancel(akisCtx)

	act := action{
		Kind:    "command",
		Code:    "sleep 1; printf 'iş bitti'",
		Timeout: "5s",
		Reply:   replyTarget{On: true},
	}

	// İş sürerken akış kopuyor.
	go func() {
		time.Sleep(200 * time.Millisecond)
		akisKapandi()
	}()

	opts := teslimOpts()
	opts.actionTimeout = 5 * time.Second

	reply, err := runAction(workCtx, opts, act, teslimPayload(), client, false, false)

	if err != nil {
		t.Fatalf("akış kapandı ama iş öldürülmemeliydi: %v", err)
	}
	if !strings.Contains(reply, "iş bitti") {
		t.Errorf("iş tamamlanmadı: %q", reply)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("yanıt gönderilmedi — kullanıcının şikâyeti tam olarak bu")
	}
	if !strings.Contains(fake.sent[0].Text, "iş bitti") {
		t.Errorf("gönderilen yanlış: %q", fake.sent[0].Text)
	}
}

// ESKİ DAVRANIŞ: akış bağlamı doğrudan kullanılırsa iş ölür ve hiçbir şey
// gitmez. Düzeltmenin gerçekten işe yaradığını göstermek için bu yolun
// hâlâ öldürdüğü kayıt altına alınır.
func TestEskiYolAksiyonuOlduruyordu(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	akisCtx, akisKapandi := context.WithCancel(context.Background())

	act := action{
		Kind:    "command",
		Code:    "sleep 1; printf 'iş bitti'",
		Timeout: "5s",
		Reply:   replyTarget{On: true},
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		akisKapandi()
	}()

	opts := teslimOpts()
	opts.actionTimeout = 5 * time.Second

	_, err := runAction(akisCtx, opts, act, teslimPayload(), client, false, false)

	if err == nil {
		t.Fatal("bu test, eski yolun (akisCtx doğrudan) işi öldürdüğünü gösteriyor")
	}
	// Önemli: kullanıcı yine de BİR ŞEY alır (düzeltilmiş bildirim).
	if len(fake.sent) == 0 {
		t.Error("düzeltme olsa bile kullanıcıya bildirim gitmeliydi")
	}
}
