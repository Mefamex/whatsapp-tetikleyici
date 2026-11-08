// whatsapp-bridge-project/tetikleyici/teslim_test.go
//
// Bu dosyanın konusu: "iş bitiyor ama WhatsApp'a ulaşmıyor".
//
// Ölçülen olgu (docs/KRITIK-SORUNLAR-2026-10-04.md #6): 124 tetiklemenin
// 25'i (%20) cevap üretmedi. runAction boş çıktıda hata döndürüyordu ve
// engine yalnız günlüğe yazıyordu — sohbete HİÇBİR ŞEY gitmiyordu.
// Kullanıcı "cevap vermiyor musun" diye soruyordu, sebebi görünmüyordu.
//
// Buradaki testler sessizliğin her yolunu kapatır.

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// teslimOpts, kısa süreli eylemler için ayarlar: komutun birkaç saniyede
// öldürülmesi sağlanır ki zaman aşımı yolu test edilebilsin.
func teslimOpts() options {
	opts := testOptions()
	opts.actionTimeout = 300 * time.Millisecond
	opts.sendTimeout = 5 * time.Second
	return opts
}

func teslimPayload() payload {
	p := testPayload()
	p.Trigger.Text = "uzun bir iş yap"
	return p
}

// ── 1. Zaman aşımında kısmi çıktı kurtarılır ve GÖNDERİLİR ──────────────

// Ajan konuştu ama iş bitmedi. Konuştuğu şey kaybolmamalı: kullanıcı yarım
// kalan işi de görmek ister, hiç görmemekten iyidir.
func TestZamanAsimindaKismiCiktiGonderilir(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	// Önce konuş, sonra uzun süre bekle: kesilecek ama çıktı üretmiş olacak.
	act := action{
		Kind:  "command",
		Code:  "printf 'dosyayı hazırlıyorum'; sleep 3; printf 'bitti'",
		Reply: replyTarget{On: true},
	}

	reply, err := runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	// Kısmi çıktı gönderildiği için bu bir BAŞARI yoludur: kullanıcı bir şey
	// aldı, hata yutulmadı. Hata dönmemesi doğru davranıştır.
	if err != nil {
		t.Fatalf("kısmi çıktı gönderildiği için hata dönmemeliydi: %v", err)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("gönderilen mesaj sayısı = %d, 1 olmalı", len(fake.sent))
	}
	if !strings.Contains(fake.sent[0].Text, "dosyayı hazırlıyorum") {
		t.Errorf("kısmi çıktı gönderilmedi, gönderilen: %q", fake.sent[0].Text)
	}
	// Kısmi çıktı gittiyse ayrıca "başarısız" bildirimi GÖNDERİLMEMELİ:
	// kullanıcı aynı anda iki mesaj almamalı.
	if strings.Contains(fake.sent[0].Text, "cevap üretilemedi") {
		t.Errorf("kısmi çıktı varken ayrıca hata bildirimi gönderilmiş: %q", fake.sent[0].Text)
	}
	if reply == "" {
		t.Error("dönen yanıt boş olmamalı (kısmi çıktı geri verilmeli)")
	}
}

// ── 2. Hiç konuşmadan öldürülen iş → kullanıcıya sebep mesajı ────────────

// Bu, kullanıcının asıl şikâyetinin karşılığı: "sadece session'a basıp
// bırakıyor, WhatsApp'a gitmiyor". Ajan tek satır bile yazmadan öldürüldüyse
// kullanıcı hiçbir şey almazdı.
func TestCiktiTamamenBossaKullaniciyaSebepGider(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "sleep 3", // hiç çıktı yazmaz, süre dolarsa öldürülür
		Reply: replyTarget{On: true},
	}

	_, err := runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if err == nil {
		t.Fatal("zaman aşımı hata vermeliydi")
	}
	if len(fake.sent) != 1 {
		t.Fatalf("kullanıcıya mesaj gitmedi (gönderilen: %d)", len(fake.sent))
	}
	if !strings.Contains(fake.sent[0].Text, "zaman aşım") {
		t.Errorf("beklenen sebep bildirimi değil: %q", fake.sent[0].Text)
	}
}

// ── 3. reply: false olan eylem bildirim GÖNDERMEZ ─────────────────────────

// Susturma kuralı (Masha "sen sus" dedi): ajan konuşsa bile sohbete
// hiçbir şey gitmemeli. Yanlışlıkla bildirim giterse susturma işe yaramaz.
func TestReplyKapaliykenHicbirMesajGonderilmez(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "sleep 3", // zaman aşımına uğrayacak, çıktı boş
		Reply: replyTarget{On: false},
	}

	_, _ = runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if len(fake.sent) != 0 {
		t.Errorf("reply kapalıyken %d mesaj gönderildi, 0 olmalıydı: %v",
			len(fake.sent), fake.sent)
	}
}

// ── 4. Başarılı yolda davranış değişmedi ─────────────────────────────────

func TestBasariliCiktiOldugundaTekMesajGider(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "printf 'tamam, hallettim'",
		Reply: replyTarget{On: true},
	}

	reply, err := runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if len(fake.sent) != 1 || fake.sent[0].Text != "tamam, hallettim" {
		t.Errorf("gönderilen: %v", fake.sent)
	}
	if reply != "tamam, hallettim" {
		t.Errorf("dönen yanıt = %q", reply)
	}
}

// Sabit metin verilmişse çıktı olmasa da o metin gönderilir: bildirim
// yoluna düşülmemelidir.
func TestSabitMetinCiktiBossaDaGonderilir(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "sleep 3",
		Reply: replyTarget{On: true, Text: "pong"},
	}

	_, _ = runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if len(fake.sent) != 1 || fake.sent[0].Text != "pong" {
		t.Errorf("sabit metin gönderilmedi: %v", fake.sent)
	}
}

// ── 5. Sebep metni iç yol/session sızdırmamalı ───────────────────────────

// Hata metni dosya yolu ya da session kimliği içerebilir. Bunlar misafire
// gönderilirse iç sistem şeması sızar (belgedeki #4'te bu yaşandı).
func TestSebepMesniIcYolSizdirmaz(t *testing.T) {
	// Zaman aşımına uğramış ve iptal edilmiş bağlamlar.
	timedOut, cancelTimedOut := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelTimedOut()
	time.Sleep(2 * time.Millisecond)

	cancelled, cancelCancelled := context.WithCancel(context.Background())
	cancelCancelled()

	hesaplar := []struct {
		ad    string
		ctx   context.Context
		err   error
		yasak []string
	}{
		{"zaman aşımı", timedOut, nil, []string{"/home/", "ses_", ".go"}},
		{"iptal", cancelled, nil, []string{"/home/", "ses_"}},
		{"signal killed", context.Background(),
			errors.New("çıkış hatası: signal: killed"), []string{"/home/", "ses_"}},
		{"rate limit", context.Background(),
			errors.New("Error: Rate limit exceeded"), []string{"/home/", "ses_"}},
		{"genel hata", context.Background(),
			errors.New("opencode /home/mfmx/x.go hata"), []string{"/home/mfmx", "x.go"}},
	}

	for _, h := range hesaplar {
		sebep := bosCiktiSebebi(h.ctx, h.err)

		if strings.TrimSpace(sebep) == "" {
			t.Errorf("%s: sebep boş", h.ad)
		}
		for _, y := range h.yasak {
			if strings.Contains(sebep, y) {
				t.Errorf("%s: sebep iç bilgi sızdırıyor (%q): %q", h.ad, y, sebep)
			}
		}
		if len(sebep) > 160 {
			t.Errorf("%s: sebep çok uzun (%d karakter): %q", h.ad, len(sebep), sebep)
		}
	}
}

// ── 6. Gönderim de başarısızsa hata yutulmaz ─────────────────────────────

func TestBildirimGonderilemezseHataYutulmaz(t *testing.T) {
	fake := &fakeBridge{failWith: http.StatusServiceUnavailable}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "sleep 3",
		Reply: replyTarget{On: true},
	}

	_, err := runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if err == nil {
		t.Fatal("gönderim başarısız olduğunda hata dönmeliydi")
	}
	if !strings.Contains(err.Error(), "gönderilemedi") {
		t.Errorf("gönderim hatası bildirilmedi: %v", err)
	}
}

// Kısmi cevap gönderildiğinde kullanıcı kesildiğini ANLAMALI. Yarım kalmış
// bir cevap tamamlanmış gibi görünürse kullanıcı yanlış bilgilenir —
// "hazır" dediği bir işin bitmediğini sanır.
func TestKismiCevapKesildiginiBelliEder(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "printf 'PDF hazırlanıyor'; sleep 3",
		Reply: replyTarget{On: true},
	}

	_, _ = runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if len(fake.sent) != 1 {
		t.Fatalf("mesaj sayısı = %d", len(fake.sent))
	}
	if !strings.Contains(fake.sent[0].Text, "PDF hazırlanıyor") {
		t.Errorf("kısmi çıktı yok: %q", fake.sent[0].Text)
	}
	if !strings.Contains(fake.sent[0].Text, "tamamlanmadı") {
		t.Errorf("kesildiği belli edilmedi: %q", fake.sent[0].Text)
	}
}

// Başarılı eylemde kesik bildirisi EKLENMEZ: kullanılmış bir işaret
// ("tamamlanmadı") yanlış alarm üretir.
func TestBasariliCevaptaKesikBildirisiOlmaz(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{
		Kind:  "command",
		Code:  "printf 'bitti, 6 sayfa'",
		Reply: replyTarget{On: true},
	}

	_, _ = runAction(context.Background(), teslimOpts(), act, teslimPayload(), client, false, false)

	if strings.Contains(fake.sent[0].Text, "tamamlanmadı") {
		t.Errorf("başarılı cevaba kesik bildirisi eklendi: %q", fake.sent[0].Text)
	}
}

// ── 7. Kural bazlı zaman aşımı ──────────────────────────────────────────
//
// "1 numaralı ödevi yap" 3 dakikayı aşabiliyor; 3 dakikada öldürülen ajanın
// emeği kayboluyordu. Uzun işler için kural kendi süresini yazabilmeli.

func TestKuralKendiZamanAsiminiUygular(t *testing.T) {
	opts := teslimOpts() // 300 ms

	kisa := action{Kind: "command", Timeout: "50ms"}
	uzun := action{Kind: "command", Timeout: "2s"}

	if got := kisa.sure(opts); got.String() != "50ms" {
		t.Errorf("kısa kural süresi = %v, 50ms olmalı", got)
	}
	if got := uzun.sure(opts); got.String() != "2s" {
		t.Errorf("uzun kural süresi = %v, 2s olmalı", got)
	}
}

// timeout yazılmamışsa bayrağın değeri geçerli.
func TestTimeoutYoksaBayrakKullanilir(t *testing.T) {
	opts := teslimOpts()

	if got := (action{Kind: "command"}).sure(opts); got != opts.actionTimeout {
		t.Errorf("süre = %v, bayrak değeri %v olmalı", got, opts.actionTimeout)
	}
}

// Yazım hatası varsayılana düşmeli — sıfıra düşürüp her şeyi öldürmemeli.
// "timeout: 15" (birim yok) en sık hata: saniye sanıldığı sanılır.
func TestHataliTimeoutVarsayilanaDuser(t *testing.T) {
	opts := teslimOpts()

	for _, bozuk := range []string{"15", "biraz", "-5m", "0", "15x"} {
		act := action{Kind: "command", Timeout: bozuk}
		if got := act.sure(opts); got != opts.actionTimeout {
			t.Errorf("timeout %q → %v, varsayılan %v olmalıydı",
				bozuk, got, opts.actionTimeout)
		}
	}
}

// Gerçek davranış: kural süresi 2 saniye, komut 1 saniyede biterse ÖLMEMELİ.
// Varsayılan 300 ms olsaydı ölür ve kullanıcıya hata giderdi.
func TestUzunKuralKisaIsiOldurmur(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	opts := teslimOpts() // 300 ms — bu süre işi öldürürdü
	act := action{
		Kind:    "command",
		Code:    "printf 'ödev hazır, 6 sayfa'",
		Timeout: "5s", // kural kendi süresini veriyor
		Reply:   replyTarget{On: true},
	}

	reply, err := runAction(context.Background(), opts, act, teslimPayload(), client, false, false)
	if err != nil {
		t.Fatalf("kural süresi yeterliydi, hata oldu: %v", err)
	}
	if reply != "ödev hazır, 6 sayfa" {
		t.Errorf("yanıt = %q", reply)
	}
	if len(fake.sent) != 1 {
		t.Errorf("mesaj sayısı = %d", len(fake.sent))
	}
}
