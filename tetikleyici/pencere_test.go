package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Bu dosya "ajan son mesajları görüyor mu" sorusunu ölçer.
//
// TODO itirazı: "her cevap öncesi tek pencerede oku; şu an tetikleyici kendi
// penceresini geçiyor, geçmişi ajana ayrıca göstermiyor". İtiraz ya doğru
// ya da hayır — varsayım değil ÖLÇÜM ister. Aşağıdaki testler istemin
// tam metnini sahte ajanın argüman dosyasından okur; yani ajanın gerçekten
// gördüğü şey ölçülür, hesaplanmaz.

// ── yardımcılar ───────────────────────────────────────────────────────────

// pencereMesaji, geçmiş dizisine eklenecek mesaj üretir.
//
// timestamp ARALIKLI verilir: sıralamanın "zaten sıralı geldiği" için
// doğru çıkması ihtimali ortadan kalkar.
func pencereMesaji(ts int64, id, text string, benden bool) message {
	m := message{
		ID: id, From: "12345678901234@lid", Sender: "12345678901234@lid",
		PushName: "Mefamex", Type: "text", Text: text,
		Timestamp: ts, Direction: "in",
	}
	if benden {
		m.Direction = "out"
		m.IsFromMe = true
		m.From = "905550000001@s.whatsapp.net"
	}
	return m
}

// ajanIstemi, sahte ajana verilen istemin tam metnini döndürür.
func ajanIstemi(t *testing.T, argsDosya string) string {
	t.Helper()

	ham, err := os.ReadFile(argsDosya)
	if err != nil {
		t.Fatalf("sahte ajan argüman dosyası yok: %v", err)
	}

	// Argümanlar NUL ayrımıyla yazılır; istem çok satırlı olduğu için.
	//
	// Dosya NUL ile BİTER (her argüman NUL ile ayrılır), bu yüzden son
	// öğe boş string'tir. "Son öğe" demek istemi atar ve test, istem
	// varken "istem yok" sanır. Sondaki boş öğeler atılır.
	parcalar := strings.Split(string(ham), "\x00")
	for len(parcalar) > 0 && parcalar[len(parcalar)-1] == "" {
		parcalar = parcalar[:len(parcalar)-1]
	}
	if len(parcalar) < 2 {
		t.Fatalf("ajan istemi yok: %q", string(ham))
	}
	return parcalar[len(parcalar)-1]
}

// ajanaSoru, sahte ajan + sahte köprü kurar ve tek kuralın eylemini verir.
//
// Dönüş: eylem, options, istem dosyası, sahte köprü.
func ajanaSoru(t *testing.T, gecmis []message) (action, options, string, *bridgeClient) {
	t.Helper()

	bin, argsDosya := sahteOpencode(t, jsonMetin("anlaşıldı"))
	kopru := &fakeBridge{gecmis: gecmis}
	client := kopru.start(t)

	ham := `opencode: ` + bin + `
okundu_isaretle: false
ai_eti_ekle: false
debug_cikti: false
rules:
  - name: mefamex
    match:
      wp_number: ["905550000001", "12345678901234@lid"]
      wp_name: Mefamex
    do:
      - type: opencode
        session: ses_pencere
        agent: wp-sahip
        reply: true
        send_seen: true
        add_aitag: false
        do_debug: false
        cmd: /tmp/opencode/yardimci.sh
        allow_help: true
        allow_cmd: true
`
	set, err := parseConfig([]byte(ham))
	if err != nil {
		t.Fatalf("config parse edilemedi: %v", err)
	}
	if len(set.Rules) != 1 {
		t.Fatalf("kural sayısı = %d", len(set.Rules))
	}

	opts := options{
		opencode:      bin,
		fetchTimeout:  2 * time.Second,
		actionTimeout: 30 * time.Second,
		// sendTimeout OLMADAN gönderim anında başarısız olur: bağlam
		// sıfır süreli deadline alır. Bu testte istem ölçülüyor, gönderim
		// değil; yine de köprü sahte olduğu için gönderim de geçmeli.
		sendTimeout: 5 * time.Second,
	}
	return set.Rules[0].Actions[0], opts, argsDosya, client
}

// ── testler ───────────────────────────────────────────────────────────────

// AJAN SON MESAJLARI GÖRMELİ.
func TestAjanSonMesajlariGoruyor(t *testing.T) {
	gecmis := []message{
		pencereMesaji(100, "e1", "ilk soru: tshirt ne boy", false),
		pencereMesaji(200, "e2", "L beden", false),
		pencereMesaji(300, "e3", "L beden alıyorum", true),
	}
	tetikleyen := pencereMesaji(400, "yeni", "kaç tane kaldı?", false)

	act, opts, argsDosya, client := ajanaSoru(t, gecmis)
	p := payload{
		Trigger: tetikleyen,
		History: sortOldestFirst(append(append([]message{}, gecmis...), tetikleyen)),
	}

	if _, err := runAction(context.Background(), opts, act, p, client, false, false); err != nil {
		t.Fatalf("eylem hatası: %v", err)
	}

	istem := ajanIstemi(t, argsDosya)
	for _, beklenen := range []string{
		"ilk soru: tshirt ne boy",
		"L beden",
		"L beden alıyorum",
		"kaç tane kaldı?",
	} {
		if !strings.Contains(istem, beklenen) {
			t.Errorf("ajan istemi %q metnini görmüyor:\n%s", beklenen, istem)
		}
	}
}

// SIRA ÖNEMLİ: karışık sırada verilen geçmiş eskiden yeniye dizilmeli.
// Sıra yanlışsa ajan "sonra ne oldu"yu yanlış anlar.
func TestAjanGecmisiEskidenYeniyeGoruyor(t *testing.T) {
	gecmis := []message{
		pencereMesaji(300, "e3", "üçüncü", false),
		pencereMesaji(100, "e1", "birinci", false),
		pencereMesaji(200, "e2", "ikinci", false),
	}
	tetikleyen := pencereMesaji(400, "yeni", "dördüncü", false)

	act, opts, argsDosya, client := ajanaSoru(t, gecmis)
	p := payload{
		Trigger: tetikleyen,
		History: sortOldestFirst(append(append([]message{}, gecmis...), tetikleyen)),
	}

	if _, err := runAction(context.Background(), opts, act, p, client, false, false); err != nil {
		t.Fatalf("eylem hatası: %v", err)
	}

	istem := ajanIstemi(t, argsDosya)
	konum := func(metin string) int { return strings.Index(istem, metin) }

	bir, iki := konum("birinci"), konum("ikinci")
	uc, dort := konum("üçüncü"), konum("dördüncü")
	if bir < 0 || iki < 0 || uc < 0 || dort < 0 {
		t.Fatalf("istem eksik (birinci=%d ikinci=%d üçüncü=%d dördüncü=%d)", bir, iki, uc, dort)
	}
	if !(bir < iki && iki < uc && uc < dort) {
		t.Errorf("sıra bozuk: birinci=%d ikinci=%d üçüncü=%d dördüncü=%d", bir, iki, uc, dort)
	}
}

// KENDİ MESAJLARIMIZ da bağlamdır: "3 tane aldım" cevabını ajan görmezse
// "kaç tane kaldı" sorusuna doğru cevap veremez.
func TestAjanKendiMesajlarimiziAyirtEdiyor(t *testing.T) {
	gecmis := []message{
		pencereMesaji(100, "e1", "sipariş ver", false),
		pencereMesaji(200, "e2", "3 tane sipariş verdim", true),
	}
	tetikleyen := pencereMesaji(300, "yeni", "peki ne zaman gelir?", false)

	act, opts, argsDosya, client := ajanaSoru(t, gecmis)
	p := payload{
		Trigger: tetikleyen,
		History: sortOldestFirst(append(append([]message{}, gecmis...), tetikleyen)),
	}

	if _, err := runAction(context.Background(), opts, act, p, client, false, false); err != nil {
		t.Fatalf("eylem hatası: %v", err)
	}

	istem := ajanIstemi(t, argsDosya)
	if !strings.Contains(istem, "me: 3 tane sipariş verdim") {
		t.Errorf("kendi mesajım 'me:' etiketiyle ayrılmadı:\n%s", istem)
	}
	if strings.Contains(istem, "Mefamex: 3 tane sipariş verdim") {
		t.Errorf("kendi mesajım benim adıma yazıldı:\n%s", istem)
	}
}

// Köprü düşerse eylem YİNE çalışmalı: kullanıcı hiçbir cevap almamamalı.
// Geçmiş lüks değil, hata değil de konuşma kaynağıdır.
func TestAjanGecmisAlinamasaCevapVeriyor(t *testing.T) {
	bin, argsDosya := sahteOpencode(t, jsonMetin("yine de cevap verdim"))
	kopru := &fakeBridge{failWith: 500}
	kopru.start(t)

	ham := `opencode: ` + bin + `
okundu_isaretle: false
ai_eti_ekle: false
debug_cikti: false
rules:
  - name: mefamex
    match:
      wp_number: ["12345678901234@lid"]
    do:
      - type: opencode
        session: ses_pencere
        agent: wp-sahip
        reply: false
        cmd: /tmp/opencode/yardimci.sh
        allow_help: true
`
	set, err := parseConfig([]byte(ham))
	if err != nil {
		t.Fatalf("config parse edilemedi: %v", err)
	}

	opts := options{
		opencode:      bin,
		fetchTimeout:  500 * time.Millisecond,
		actionTimeout: 30 * time.Second,
		sendTimeout:   5 * time.Second,
	}
	// History YİNE doldurulur: buildPrompt yalnız History'e bakar. Yalnız
	// Trigger verilseydi istem boş çıkar ve test "geçmiş gitmedi" sanardı —
	// oysa geçmiş hiç denenmemişti.
	tetikleyen := pencereMesaji(100, "y", "merhaba", false)
	p := payload{Trigger: tetikleyen, History: []message{tetikleyen}}

	if _, err := runAction(context.Background(), opts, set.Rules[0].Actions[0], p, nil, false, false); err != nil {
		t.Fatalf("geçmiş reddedildiği için eylem hata vermemeliydi: %v", err)
	}
	if !strings.Contains(ajanIstemi(t, argsDosya), "merhaba") {
		t.Error("geçmiş alınamasa bile tetikleyen mesaj ajana gitmeli")
	}
}

// ── pencerenin kendisi ────────────────────────────────────────────────────

// Pencere köprüye count olarak GİTMELİ: sınırsız geçmiş istemi şişirir,
// ajanı boğar ve kotayı harcar. Varsayılan 5.
func TestPencereKopruyeSinirliSayidaGidiyor(t *testing.T) {
	for _, pencere := range []int{3, 5} {
		t.Run(itoa(pencere), func(t *testing.T) {
			gecmis := make([]message, 0, 20)
			for n := range 20 {
				gecmis = append(gecmis, pencereMesaji(int64(100+n), "e"+itoa(n), "mesaj "+itoa(n), false))
			}

			var istenen []string
			kopru := &fakeBridge{gecmis: gecmis, sayacGoruldu: func(c *bridgeClient, jid, count string) {
				istenen = append(istenen, count)
			}}
			client := kopru.start(t)

			eng := &engine{
				configPath: "test.yaml",
				opts:       options{fetchTimeout: 2 * time.Second},
				window:     pencere,
			}
			eng.mu.Lock()
			eng.last = ruleSet{Rules: []rule{{
				Name:    "mefamex",
				Match:   condition{Numbers: stringList{"12345678901234@lid"}},
				Actions: []action{{Kind: "command", Code: "true"}},
			}}}
			eng.lastOK = true
			eng.mu.Unlock()

			eng.buildPayload(context.Background(), client, pencereMesaji(999, "yeni", "şimdi", false))

			if len(istenen) != 1 {
				t.Fatalf("köprü %d kez sorgulandı, want 1", len(istenen))
			}
			if istenen[0] != itoa(pencere) {
				t.Errorf("köprüye giden count = %q, want %d", istenen[0], pencere)
			}
		})
	}
}

// Pencere kapalı (0) olduğunda köprüye hiç gidilmemeli: kullanıcı "yalnız
// tetikleyen mesajı" dediyse geçmiş almak gereksiz yük.
func TestPencereKapaliykenKopruyeGitmiyor(t *testing.T) {
	var istenen int
	kopru := &fakeBridge{gecmis: []message{pencereMesaji(100, "e1", "eski", false)}}
	kopru.sayacGoruldu = func(c *bridgeClient, jid, count string) { istenen++ }
	client := kopru.start(t)

	eng := &engine{configPath: "test.yaml", opts: options{fetchTimeout: time.Second}}
	eng.mu.Lock()
	eng.lastOK = true
	eng.mu.Unlock()

	tetikleyen := pencereMesaji(200, "yeni", "yalnız bu", false)
	p := eng.buildPayload(context.Background(), client, tetikleyen)

	if istenen != 0 {
		t.Errorf("kapalı pencerede köprü %d kez sorgulandı, want 0", istenen)
	}
	if len(p.History) != 1 || p.History[0].ID != "yeni" {
		t.Errorf("geçmiş yalnız tetikleyeni içermeli, %d mesaj var", len(p.History))
	}
}

// Tetikleyen mesaj geçmişte YOKSA başa eklenmeli: ajan "ne oldu" sorusunun
// cevabını görmeli.
func TestPencereTetikleyeniIcermekZorunda(t *testing.T) {
	kopru := &fakeBridge{gecmis: []message{pencereMesaji(100, "e1", "geçmişteki", false)}}
	client := kopru.start(t)

	eng := &engine{configPath: "test.yaml", opts: options{fetchTimeout: time.Second}, window: 5}
	eng.mu.Lock()
	eng.lastOK = true
	eng.mu.Unlock()

	tetikleyen := pencereMesaji(999, "canli", "akıştan gelen", false)
	p := eng.buildPayload(context.Background(), client, tetikleyen)

	var bulundu bool
	for _, m := range p.History {
		if m.ID == "canli" {
			bulundu = true
		}
	}
	if !bulundu {
		t.Errorf("tetikleyen mesaj geçmişte yok, history=%d mesaj", len(p.History))
	}
}

// itoa, küçük sayı dönüştürücü (testlerde strconv yerine okunur olsun diye).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var bayt []byte
	negatif := n < 0
	if negatif {
		n = -n
	}
	for n > 0 {
		bayt = append([]byte{byte('0' + n%10)}, bayt...)
		n /= 10
	}
	if negatif {
		return "-" + string(bayt)
	}
	return string(bayt)
}
