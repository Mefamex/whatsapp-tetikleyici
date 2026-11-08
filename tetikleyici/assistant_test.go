// whatsapp-bridge-project/tetikleyici/assistant_test.go
//
// Tek kişi tek kural: `type: opencode` yönlendirmesi.

package main

import (
	"os"
	"strings"
	"testing"
)

// tamYetenekliEylem, sahibin config'indeki tek kuralın karşılığıdır: bütün
// hatlar açık. Testler önce bununla çalışır, sonra tek tek bayrak kapatılarak
// "kapalı hat sessizdir" ilkesi sınanır.
func tamYetenekliEylem() action {
	evet := true
	return action{
		Kind:          "opencode",
		SessionID:     "ses_ef8590662ffeRSLXLL2R8VhJfM",
		Agent:         "wp-sahip",
		Timeout:       "12m",
		Cmd:           "/proj/tetikleyici/cmd/cmd.py",
		Gorsel:        "/proj/gorsel_pipeline.py",
		Ses:           "/proj/voice_pipeline.py",
		SendSeen:      &evet,
		AllowHelp:     true,
		AllowCmd:      true,
		AllowDurum:    true,
		AllowCamera:   true,
		AllowGorsel:   true,
		AllowSes:      true,
		AllowBelge:    true,
		AllowReaction: true,
	}
}

// mesaj, verilen tür ve metinle bir sohbet mesajı kurar.
func turMesaji(tur, metin string) message {
	m := mefamexMessage()
	m.Type = tur
	m.Text = metin
	return m
}

// ── yönlendirme ───────────────────────────────────────────────────────────

func TestYaziMesajiOturumaGider(t *testing.T) {
	y, ok := tamYetenekliEylem().yonlendir(turMesaji("text", "müfredat nedir"))
	if !ok {
		t.Fatal("yazılı mesaj yönlendirilmeli")
	}
	if y.tip != "session" {
		t.Errorf("tip = %q, want session", y.tip)
	}
}

func TestUzatilmisYaziVeAnketOturumaGider(t *testing.T) {
	for _, tur := range []string{"extended_text", "poll"} {
		if y, ok := tamYetenekliEylem().yonlendir(turMesaji(tur, "içerik")); !ok || y.tip != "session" {
			t.Errorf("%s → oturum olmalı, got %+v (ok=%v)", tur, y, ok)
		}
	}
}

// Eski düzende bu mesajların her biri AYRI bir kural gerektiriyordu
// (mefamex-oturum, mefamex-gorsel, mefamex-sesli-not, mefamex-belge…). Tek kural
// hepsini kapsıyor mu — sorusu bu testin konusu.
func TestTekKuralEskiKurallarinHepsiniKapsar(t *testing.T) {
	act := tamYetenekliEylem()

	vakalar := []struct {
		tip    string
		metin  string
		harfAd string
	}{
		{"text", "müfredat nedir", "oturum"},
		{"text", "/help", "/help"},
		{"text", "/camera", "/camera"},
		{"text", "/durum", "/durum"},
		{"text", "/cmd ls", "/cmd"},
		{"text", "/bilinmeyen", "/bilinmeyen"},
		{"image", "", "image hattı"},
		{"video", "", "video hattı"},
		{"sticker", "", "sticker hattı"},
		{"audio", "", "ses hattı"},
		{"document", "", "belge hattı"},
		{"reaction", "👍", "reaksiyon → oturum"},
		{"poll_vote", "Seçenek A", "anket oyu → oturum"},
	}

	for _, tc := range vakalar {
		y, ok := act.yonlendir(turMesaji(tc.tip, tc.metin))
		if !ok {
			t.Errorf("%s %q → yön yok", tc.tip, tc.metin)
			continue
		}
		if y.ad != tc.harfAd {
			t.Errorf("%s %q → hat %q, want %q", tc.tip, tc.metin, y.ad, tc.harfAd)
		}
	}
}

// Kapalı hat kuralı: ne cevap gider ne ajan uyarılır. En sık yanlış anlaşılan
// nokta burası — "cevap yok" ile "iş başarısız" aynı şey değil.
func TestKapaliIzinSessizGecer(t *testing.T) {
	vakalar := []struct {
		ad    string
		kapat func(*action)
		tip   string
		metin string
	}{
		{"/help", func(a *action) { a.AllowHelp = false }, "text", "/help"},
		{"/cmd", func(a *action) { a.AllowCmd = false }, "text", "/cmd ls"},
		{"/durum", func(a *action) { a.AllowDurum = false }, "text", "/durum"},
		{"/camera", func(a *action) { a.AllowCamera = false }, "text", "/camera"},
		{"görsel", func(a *action) { a.AllowGorsel = false }, "image", ""},
		{"ses", func(a *action) { a.AllowSes = false }, "audio", ""},
		{"belge", func(a *action) { a.AllowBelge = false }, "document", ""},
		{"reaksiyon", func(a *action) { a.AllowReaction = false }, "reaction", "👍"},
	}

	for _, tc := range vakalar {
		act := tamYetenekliEylem()
		tc.kapat(&act)
		if y, ok := act.yonlendir(turMesaji(tc.tip, tc.metin)); ok {
			t.Errorf("%s kapalıyken yön bulundu: %+v", tc.ad, y)
		}
	}
}

// Belge hattı kendi bayrağına bağlı; görsel bayrağı kapalı olsa bile PDF'in
// hattı açık kalır (ikisi aynı betiği kullansa da).
func TestBelgeVeGorselBirbirindenBagimsiz(t *testing.T) {
	act := tamYetenekliEylem()
	act.AllowGorsel = false

	if _, ok := act.yonlendir(turMesaji("document", "")); !ok {
		t.Error("görsel kapalıyken belge hattı çalışmalı")
	}
	if _, ok := act.yonlendir(turMesaji("image", "")); ok {
		t.Error("görsel kapalıyken fotoğraf yönlenmemeli")
	}
}

// İç trafik yönlendirmeye girmemeli: ajanın geri alma bildirimlerini
// "sorular" sanması anlamsız işler çıkarırdı.
func TestIcTrafikSessiz(t *testing.T) {
	for _, tur := range []string{"protocol", "poll_update", "noPayload", "unknown"} {
		if y, ok := tamYetenekliEylem().yonlendir(turMesaji(tur, "x")); ok {
			t.Errorf("%q iç trafik yönlenmemeli, got %+v", tur, y)
		}
	}
}

// Tip alanı boş ama içerik varsa mesaj yutulmamalı. Köprü bugün tip dolduruyor
// ama bu alan köprünün çıktısına bağlı; alan çıkarılırsa kullanıcının yazdığı
// metin ajana hiç ulaşmaz.
func TestTipBoshAmaMetinVarsaOturumaGider(t *testing.T) {
	y, ok := tamYetenekliEylem().yonlendir(turMesaji("", "müfredat nedir"))
	if !ok || y.tip != "session" {
		t.Errorf("tipsiz ama içerikli mesaj oturuma gitmeli, got %+v (ok=%v)", y, ok)
	}
}

// Tip de metin de yoksa gerçek bir mesaj değildir: sessiz geçilir.
func TestTipVeMetinBosturSessiz(t *testing.T) {
	if y, ok := tamYetenekliEylem().yonlendir(turMesaji("", "   ")); ok {
		t.Errorf("tamamen boş mesaj yönlenmemeli, got %+v", y)
	}
}

// Yazı tipinde olmayan boş mesaj ("") iç trafiktir; ama köprü ileride yeni bir
// metin tipi eklerse sessizce yutulmamalı — en azından oturuma düşmeli.
func TestBilinmeyenTipSessizceYutulmaz(t *testing.T) {
	if y, ok := tamYetenekliEylem().yonlendir(turMesaji("sticker_video", "")); ok {
		t.Errorf("bilinmeyen tip yönlenmemeli, got %+v", y)
	}
}

// ── betik argümanları ─────────────────────────────────────────────────────

// Argümanları config yazmayacak: eylem hangi hattı açtığını biliyor, argümanı
// da o bilmeli. `{"$TRIGGER_TEXT"}` yazmayı unutmak medya hattında 404 verirdi.
func TestKomutHattiMetniArgumanYapar(t *testing.T) {
	act := tamYetenekliEylem()
	y, _ := act.yonlendir(turMesaji("text", "/help"))

	got := act.script(y)
	want := `/proj/tetikleyici/cmd/cmd.py "$TRIGGER_TEXT" "$TRIGGER_JID" "$TRIGGER_NAME"`
	if got != want {
		t.Errorf("komut satırı:\n got %q\nwant %q", got, want)
	}
}

func TestMedyaHatlarinaKimlikArgumanGider(t *testing.T) {
	act := tamYetenekliEylem()

	for _, tc := range []struct{ tip, want string }{
		{"image", `/proj/gorsel_pipeline.py "$TRIGGER_ID"`},
		{"document", `/proj/gorsel_pipeline.py "$TRIGGER_ID"`},
		{"video", `/proj/gorsel_pipeline.py "$TRIGGER_ID"`},
		{"sticker", `/proj/gorsel_pipeline.py "$TRIGGER_ID"`},
		{"audio", `/proj/voice_pipeline.py "$TRIGGER_ID"`},
	} {
		y, ok := act.yonlendir(turMesaji(tc.tip, ""))
		if !ok {
			t.Errorf("%s yönlenmedi", tc.tip)
			continue
		}
		if got := act.script(y); got != tc.want {
			t.Errorf("%s → %q, want %q", tc.tip, got, tc.want)
		}
	}
}

func TestBosBetikScriptUretmez(t *testing.T) {
	if got := (action{}).script(yol{betik: "   "}); got != "" {
		t.Errorf("boş betik → boş komut, got %q", got)
	}
}

func TestIlkKelimeKomutuAyirir(t *testing.T) {
	for _, tc := range []struct{ giris, kalan string }{
		{"/camera", "/camera"},
		{"  /help", "/help"},
		{"/cmd ls -la", "/cmd"},
		{"/", "/"},
	} {
		if got := ilkKelime(tc.giris); got != tc.kalan {
			t.Errorf("ilkKelime(%q) = %q, want %q", tc.giris, got, tc.kalan)
		}
	}
}

// ── config doğrulama ──────────────────────────────────────────────────────

func TestOpencodeEylemiEksikAlanlariIsimleBildirir(t *testing.T) {
	vakalar := []struct {
		ad   string
		act  action
		icer string
	}{
		{
			"oturumsuz",
			action{Kind: "opencode", AllowHelp: true, Cmd: "x.sh"},
			"session required",
		},
		{
			"izin bayrağı yok",
			action{Kind: "opencode", SessionID: "ses_x"},
			"allow_*",
		},
		{
			"cmd betiği yok",
			action{Kind: "opencode", SessionID: "ses_x", AllowHelp: true},
			"cmd required",
		},
		{
			"görsel betiği yok",
			action{Kind: "opencode", SessionID: "ses_x", AllowGorsel: true},
			"gorsel required",
		},
		{
			"ses betiği yok",
			action{Kind: "opencode", SessionID: "ses_x", AllowSes: true},
			"ses required",
		},
	}

	for _, tc := range vakalar {
		err := tc.act.check()
		if err == nil {
			t.Errorf("%s: hata bekleniyordu", tc.ad)
			continue
		}
		if !strings.Contains(err.Error(), tc.icer) {
			t.Errorf("%s: hata %q, %q içermeli", tc.ad, err, tc.icer)
		}
	}
}

func TestTamYetenekliEylemGecerlidir(t *testing.T) {
	if err := tamYetenekliEylem().check(); err != nil {
		t.Errorf("tam yetenekli eylem geçerli olmalı: %v", err)
	}
}

func TestNewSessionOturumSabitlemez(t *testing.T) {
	// new_session bilerek oturum bağlamaz; alan kalmışsa kullanıcı ya yanlış
	// tür seçmiş ya da eski config'ten artık var — ikisi de sessizce yanlış
	// davranış üretir.
	err := action{Kind: "new_session", SessionID: "ses_x"}.check()
	if err == nil || !strings.Contains(err.Error(), "must not pin a session") {
		t.Errorf("yeni oturumda sabit session reddedilmeli, got %v", err)
	}
}

// ── wp_number / wp_name takma adları ──────────────────────────────────────

func TestWpNumberWpNameTekKaynagaBirlesir(t *testing.T) {
	var c condition
	c.WPNumber = stringList{"905550000001", "12345678901234@lid"}
	c.WPName = stringList{"Mefamex"}
	c.normalize()

	if len(c.Numbers) != 2 {
		t.Errorf("Numbers = %v, want 2 eleman", c.Numbers)
	}
	if len(c.Names) != 1 {
		t.Errorf("Names = %v, want 1 eleman", c.Names)
	}
	if c.WPNumber != nil || c.WPName != nil {
		t.Error("takma adlar birleştikten sonra boşaltılmalı")
	}
}

func TestEskiNumberYazimiCalismayaDevamEder(t *testing.T) {
	var c condition
	c.Numbers = stringList{"905550000001"}
	c.WPNumber = stringList{"12345678901234@lid"}
	c.normalize()

	if got := strings.Join(c.Numbers, ","); got != "905550000001,12345678901234@lid" {
		t.Errorf("Numbers = %q", got)
	}
}

// ── config dosyasından okuma ───────────────────────────────────────────────

func TestTekKuralDosyadanOkunur(t *testing.T) {
	yaml := `
opencode: ~/.opencode/bin/opencode
rules:
  - name: mefamex
    match:
      wp_number: ["905550000001", "12345678901234@lid"]
      wp_name: Mefamex
    do:
      - type: opencode
        session: ses_ef8590662ffeRSLXLL2R8VhJfM
        reply: true
        agent: wp-sahip
        timeout: 12m
        send_seen: true
        add_aitag: false
        do_debug: false
        cmd: ~/proj/tetikleyici/cmd/cmd.py
        gorsel: ~/proj/gorsel_pipeline.py
        ses: ~/proj/voice_pipeline.py
        allow_help: true
        allow_cmd: true
        allow_durum: true
        allow_gorsel: true
        allow_ses: true
        allow_camera: true
        allow_reaction: true
        allow_belge: true
`
	set, err := parseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if len(set.Rules) != 1 {
		t.Fatalf("kural sayısı = %d, want 1", len(set.Rules))
	}

	act := set.Rules[0].Actions[0]
	if act.Kind != "opencode" {
		t.Errorf("eylem tipi = %q", act.Kind)
	}
	if act.SendSeen == nil || !*act.SendSeen {
		t.Error("send_seen: true okunmalı")
	}
	// add_aitag / do_debug yazıldığı için global değer GEÇERSİZ olmalı:
	// ikisi de false yazıldığı için işaretçi nil değil, false'a işaret etmeli.
	if act.AddAITag == nil || *act.AddAITag {
		t.Error("add_aitag: false işaretçisi nil olmamalı ve false olmalı")
	}
	if act.DoDebug == nil || *act.DoDebug {
		t.Error("do_debug: false işaretçisi nil olmamalı ve false olmalı")
	}

	// wp_number → number birleşmesi dosyadan okuma yolunda da geçerli.
	if len(set.Rules[0].Match.Numbers) != 2 {
		t.Errorf("Numbers = %v, want 2", set.Rules[0].Match.Numbers)
	}
}

func TestYazimHatasiYineReddedilir(t *testing.T) {
	// KnownFields sayesinde "wp_nubmer" sessizce yok sayılmaz.
	yaml := `
rules:
  - name: hatali
    match:
      wp_nubmer: ["905550000001"]
    do:
      - type: command
        code: "true"
`
	if _, err := parseConfig([]byte(yaml)); err == nil {
		t.Error("bilinmeyen alan reddedilmeli")
	}
}

// ── opencode JSON akışı: ara mesajlar ─────────────────────────────────────

// Gerçek akıştan alınmış satırlar (2026-10-06, `opencode run --format json`).
const akisOrnegi = `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Bir saniye, bakıyorum."}}
{"type":"tool_use","part":{"type":"tool","tool":"shell","state":{"status":"completed","output":"klasor listesi burada"}}}
{"type":"step_finish","part":{"type":"step-finish","reason":"tool-calls"}}
{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"Toplam 23.050 TL."}}`

func TestToolCiktisiCevapSanilmaz(t *testing.T) {
	got := sonCevap(akisOrnegi, nil)
	if got != "Toplam 23.050 TL." {
		t.Errorf("cevap = %q, want nihai metin", got)
	}
	if strings.Contains(got, "klasor listesi") {
		t.Error("tool çıktısı cevaba karışmamalı")
	}
}

// TODO maddesi: "opencode run stdout'a ajanın TÜM mesajlarını yazıyor → kullanıcı
// tek mesajda 'bir saniye' + asıl cevabı alıyor". do_debug kapalıyken geri çağırma
// HİÇ OLUŞMAZ (araGonderici nil döner) → yalnız son cevap gider.
func TestAraMesajVarsayilanOlarakGitmez(t *testing.T) {
	// Varsayılan (do_debug: false) → geri çağırma yok.
	if araGonderici(t.Context(), testOptions(), nil, testPayload(), false) != nil {
		t.Error("do_debug kapalıyken ara mesaj geri çağırması oluşmamalı")
	}

	// Geri çağırma da olsa, sonCevap kendiliğinden ara mesaj göndermez:
	// kararı çağıran taraf verir.
	got := sonCevap(akisOrnegi, nil)
	if got != "Toplam 23.050 TL." {
		t.Errorf("cevap = %q", got)
	}
}

// do_debug: true → düşünme ve tool çıktısı hariç TÜM ara mesajlar gider.
func TestAraMesajAcikkenAraMesajlarGonderilir(t *testing.T) {
	var gonderilen []string
	son := sonCevap(akisOrnegi, func(m string) { gonderilen = append(gonderilen, m) })

	if len(gonderilen) != 1 || gonderilen[0] != "Bir saniye, bakıyorum." {
		t.Errorf("ara mesajlar = %v", gonderilen)
	}
	if son != "Toplam 23.050 TL." {
		t.Errorf("nihai cevap = %q", son)
	}
}

func TestTekMetinliAlandaAraMesajYoktur(t *testing.T) {
	tek := `{"type":"text","part":{"type":"text","text":"Tek cevap."}}`

	var gonderilen []string
	if got := sonCevap(tek, func(m string) { gonderilen = append(gonderilen, m) }); got != "Tek cevap." {
		t.Errorf("cevap = %q", got)
	}
	if len(gonderilen) != 0 {
		t.Errorf("tek parçada ara mesaj olmamalı, got %v", gonderilen)
	}
}

// JSON okunamıyorsa ham çıktı aynen döner: eski opencode sürümü sessizce
// "boş cevap" vermemeli.
func TestJSONOlamazsaHamCiktiDoner(t *testing.T) {
	if got := sonCevap("düz metin cevap\n", nil); got != "düz metin cevap" {
		t.Errorf("ham çıktı = %q", got)
	}
}

func TestHataJSONCevapSanilmaz(t *testing.T) {
	hata := `{"type":"error","timestamp":1794100326509,"sessionID":"ses_ef8590662ffeRSLXLL2R8VhJfM","error":{"type":"provider.no-route","message":"Model unavailable: opencode/nemotron-3-ultra-free"}}`
	if got := sonCevap(hata, nil); got != "" {
		t.Errorf("hata JSON çıktısı boş metin dönmeli, got %q", got)
	}
}

func TestBozukSatirlarAkisiBozmaz(t *testing.T) {
	akis := `{"type":"step_start"
{"type":"bilinmeyen_yeni_tip","part":{"type":"text","text":"görülmemeli"}}
{"type":"text","part":{"type":"text","text":"görünmeli"}}`

	got := sonCevap(akis, nil)
	if got != "görünmeli" {
		t.Errorf("cevap = %q — bozuk satır akışı bozdu", got)
	}
}

// Düşünme bloğu `--format json` akışında ayrı bir parça olarak gelebilir; o da
// elenmelidir.
func TestDusunmeBlokuCevabaKarismaz(t *testing.T) {
	akis := `{"type":"reasoning","part":{"type":"reasoning","text":"kullanıcı ne istedi ki"}}
{"type":"text","part":{"type":"text","text":"Net cevap."}}`

	if got := sonCevap(akis, nil); got != "Net cevap." {
		t.Errorf("cevap = %q", got)
	}
}

// ── uçtan uca: runAction ──────────────────────────────────────────────────

// sahteOpencode, `opencode run --format json` çıktısı üreten bir betik yazar.
// Gerçek ajana ihtiyaç duymadan akışın tamamını test edebilmek için.
//
// Çağırdığı argümanları args dosyasına yazar; argüman sırasını test eden
// testler bunu okur (opencode mesajı SONDAN okur — yanlış sırada prompt
// kaybolur).
func sahteOpencode(t *testing.T, cikti string) (bin, argsDosya string) {
	t.Helper()
	yon := t.TempDir()
	bin = yon + "/opencode"
	argsDosya = yon + "/args.txt"

	// Çıktı here-document ile basılır. Gövde olarak shell DEĞİL doğrudan
	// çıktı verilir: JSON akışı çok satırlı olduğu için shell kaçışlarıyla
	// uğraşmak hataya açıktı (her satır "command not found" oluyordu).
	//
	// Delimiter tırnaksız: $1 gibi konum parametreleri genişlesin diye.
	// Test metinlerinde $ ve ` yok, genişleme beklenmeyen yan etki yok.
	script := "#!/bin/sh\n" +
		": > " + argsDosya + "\n" +
		"for a in \"$@\"; do printf '%s\\0' \"$a\" >> " + argsDosya + "; done\n" +
		"cat <<OPENCODE_SONUC\n" + cikti + "\nOPENCODE_SONUC\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, argsDosya
}

// jsonMetin, tek parçalı bir ajan cevabının JSON akışını üretir.
func jsonMetin(metin string) string {
	return `{"type":"text","part":{"type":"text","text":"` + metin + `"}}`
}

// acıkKular, sahte opencode + sahte komut betikleriyle donatılmış tek kural.
func acıkKular(t *testing.T, govde string) (action, options) {
	t.Helper()
	act := tamYetenekliEylem()
	act.DoDebug = nil // global bayrağı kullansın

	act.Cmd, _ = sahteOpencode(t, jsonMetin("/help çalıştı"))
	act.Gorsel, _ = sahteOpencode(t, "görsel hattı: $1")
	act.Ses, _ = sahteOpencode(t, "ses hattı: $1")

	opts := replyOptions()
	opts.opencode, _ = sahteOpencode(t, govde)
	return act, opts
}

func TestYaziMesajiAjanCevabiSohbeteGider(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("23.050 TL"))
	act.Reply = replyTarget{On: true}

	fake := &fakeBridge{}
	yanit, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, false)
	if err != nil {
		t.Fatalf("runAction: %v", err)
	}

	if yanit != "23.050 TL" {
		t.Errorf("dönen cevap = %q", yanit)
	}
	if len(fake.sent) != 1 || fake.sent[0].Text != "23.050 TL" {
		t.Errorf("gönderilen = %+v", fake.sent)
	}
	// Cevap, tetikleyen sohbete gider.
	if fake.sent[0].To != "12345678901234@lid" {
		t.Errorf("alıcı = %q", fake.sent[0].To)
	}
}

// send_seen: true → mavi tik. Yanlışlıkla gönderim sayılmamalı.
func TestSendSeenOkunduIsaretiGonderir(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("tamam"))
	evet := true
	act.SendSeen = &evet

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}

	if len(fake.isaret) != 1 {
		t.Fatalf("işaretleme sayısı = %d, want 1", len(fake.isaret))
	}
	if fake.isaret[0].JID != "12345678901234@lid" {
		t.Errorf("işaret JID = %q", fake.isaret[0].JID)
	}
	if len(fake.isaret[0].MessageIDs) == 0 {
		t.Error("mesaj kimliği gönderilmedi")
	}
}

// send_seen YAZILMAMIŞSA hiçbir şey yapılmaz: global ayar motor seviyesinde.
func TestSendSeenYazilmadikçaIstekGitmez(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("tamam"))
	act.SendSeen = nil

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.isaret) != 0 {
		t.Errorf("send_seen yokken işaretleme yapılmamalı, got %v", fake.isaret)
	}
}

// add_aitag: false, global açıkken bile kapatmalı: kişi bazlı karar globali
// geçersiz kılar.
func TestAddAITagEylemGlobaliGecersizKilar(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("cevap"))
	act.Reply = replyTarget{On: true}
	hayir := false
	act.AddAITag = &hayir

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), true, false); err != nil {
		t.Fatal(err)
	}

	if len(fake.sent) != 1 {
		t.Fatalf("sent = %+v", fake.sent)
	}
	if strings.Contains(fake.sent[0].Text, "🤖") {
		t.Errorf("add_aitag: false iken etiket eklenmemeli: %q", fake.sent[0].Text)
	}
}

// Yazılmadığında global açık kalmalı.
func TestAddAITagYazilmadikcaGlobalGecerli(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("cevap"))
	act.Reply = replyTarget{On: true}
	act.AddAITag = nil

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), true, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0].Text, "🤖") {
		t.Errorf("global ai_eti_ekle geçerli olmalı: %+v", fake.sent)
	}
}

// do_debug: false iken ara mesaj gitmez — bu, "kullanıcı tek mesajda 'bir saniye'
// + cevap alıyor" şikâyetinin düzeltilmiş hâlidir.
func TestDoDebugKapaliykenAraMesajGitmez(t *testing.T) {
	act, opts := acıkKular(t, akisOrnegi)
	act.Reply = replyTarget{On: true}
	hayir := false
	act.DoDebug = &hayir

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, true); err != nil {
		t.Fatal(err)
	}

	if len(fake.sent) != 1 {
		t.Fatalf("sent = %d, want 1 (yalnız nihai cevap): %+v", len(fake.sent), fake.sent)
	}
	if fake.sent[0].Text != "Toplam 23.050 TL." {
		t.Errorf("gönderilen = %q", fake.sent[0].Text)
	}
}

// do_debug: true → ara mesajlar AYRI mesajlar olarak, tool çıktısı OLMADAN.
func TestDoDebugAcikkenAraMesajlarAyrıGider(t *testing.T) {
	act, opts := acıkKular(t, akisOrnegi)
	act.Reply = replyTarget{On: true}
	evet := true
	act.DoDebug = &evet

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}

	if len(fake.sent) != 2 {
		t.Fatalf("sent = %d, want 2 (ara mesaj + nihai): %+v", len(fake.sent), fake.sent)
	}
	if fake.sent[0].Text != "Bir saniye, bakıyorum." {
		t.Errorf("ara mesaj = %q", fake.sent[0].Text)
	}
	if fake.sent[1].Text != "Toplam 23.050 TL." {
		t.Errorf("nihai cevap = %q", fake.sent[1].Text)
	}
	for _, m := range fake.sent {
		if strings.Contains(m.Text, "klasor listesi") {
			t.Errorf("tool çıktısı sohbete sızmamalı: %q", m.Text)
		}
	}
}

// Kapalı hat: ne cevap, ne "iş yapılmadı" bildirimi, ne ajan uyarısı.
//
// Metin "/help" OLMALI: kapatılan yetenek /help, düz metin ise ajan hattı
// açık kalır ve cevap beklenen davranış odur.
func TestKapaliHatHicbirSeyGondermez(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("ajan konuştu"))
	act.AllowHelp = false
	act.Reply = replyTarget{On: true}

	p := testPayload()
	p.Trigger.Text = "/help"

	fake := &fakeBridge{}
	yanit, err := runAction(t.Context(), opts, act, p, fake.start(t), false, false)
	if err != nil {
		t.Fatalf("kapalı hat hata vermemeli: %v", err)
	}
	if yanit != "" {
		t.Errorf("yanıt = %q, want boş", yanit)
	}
	if len(fake.sent) != 0 {
		t.Errorf("kapalı hatta mesaj gönderilmemeli: %+v", fake.sent)
	}
}

// Medya hattı doğru betiğe, doğru argümanla gider.
func TestMedyaHattiDogruBetikVeArgumanCagirir(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("görüntüledim"))
	act.Reply = replyTarget{On: true}

	p := testPayload()
	p.Trigger.Type = "image"

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, p, fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}

	if len(fake.sent) != 1 || fake.sent[0].Text != "görsel hattı: W1" {
		t.Errorf("görsel hattı çıktısı = %+v", fake.sent)
	}
}

// Ajan çalıştırılmadan önce yön seçilir: kapalı medya hattı ajanı UYANDIRMAMALI
// (yoksa ajan boşuna token harcardı).
func TestKapaliMedyaHattiAjanCagirmaz(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("ajan konuştu"))
	act.AllowGorsel = false
	act.Reply = replyTarget{On: true}

	p := testPayload()
	p.Trigger.Type = "image"

	fake := &fakeBridge{}
	// opencode betiği hiç çalışmamalı; args.txt hiç oluşmamalı.
	if _, err := runAction(t.Context(), opts, act, p, fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 0 {
		t.Errorf("kapalı medya hattı sessiz olmalı: %+v", fake.sent)
	}
}

// opencode mesajı SONDAN okur. `--format json` eklenirken prompt'un yerini
// kaydırmak, ajana komut satırını prompt sanmasına yol açardı.
func TestPromptArgumanlardaSonSiraKalir(t *testing.T) {
	bin, argsDosya := sahteOpencode(t, jsonMetin("cevap"))

	act := tamYetenekliEylem()
	act.Cmd = bin
	act.Gorsel = bin
	act.Ses = bin

	opts := replyOptions()
	opts.opencode = bin

	p := testPayload()
	p.Trigger.Type = "text"
	p.Trigger.Text = "müfredat nedir"

	if _, err := runAction(t.Context(), opts, act, p, (&fakeBridge{}).start(t), false, false); err != nil {
		t.Fatal(err)
	}

	args := okuArgumanlar(t, argsDosya)

	son := args[len(args)-1]
	if !strings.Contains(strings.ToLower(son), "üfredat nedir") {
		t.Errorf("son argüman prompt olmalı, got %q", son)
	}
	if !icerirMi(args, "--format") {
		t.Errorf("--format json geçirilmeli: %v", args)
	}
	if !icerirMi(args, "--agent") || !icerirMi(args, "wp-sahip") {
		t.Errorf("--agent wp-sahip geçirilmeli: %v", args)
	}
	if !icerirMi(args, "--session") {
		t.Errorf("--session geçirilmeli: %v", args)
	}
}

// icerirMi, listede tam bu değer var mı diye bakar.
func icerirMi(liste []string, aranan string) bool {
	for _, v := range liste {
		if v == aranan {
			return true
		}
	}
	return false
}

// okuArgumanlar, sahte opencode'un args dosyasını okur.
//
// Ayırıcı NUL: prompt ÇOK SATIRLIDIR (tüm konuşma geçmişi gömülü), satır
// ayırıcısı kullanılsaydı tek argüman on satır sanılırdı.
func okuArgumanlar(t *testing.T, dosya string) []string {
	t.Helper()
	ham, err := os.ReadFile(dosya)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	for _, a := range strings.Split(string(ham), "\x00") {
		if a != "" {
			args = append(args, a)
		}
	}
	return args
}

// ── açıklama ──────────────────────────────────────────────────────────────

func TestAciklamaAcikVeKapaliyiAyirir(t *testing.T) {
	act := tamYetenekliEylem()
	act.AllowSes = false

	got := act.aciklama()
	if !strings.Contains(got, "açık:") || !strings.Contains(got, "sesli not") {
		t.Errorf("açık listede sesli not olmamalı: %s", got)
	}
	if !strings.Contains(got, "kapalı: sesli not") {
		t.Errorf("kapalı listesi yanlış: %s", got)
	}
}

// TestKapaliHatMaviTikDeBasmaz, kapalı hatta mesajın OKUNDU da
// işaretlenmediğini — yani susturmanın yalnız cevabı değil, hattın
// *varlığını* bile belli eden tüm yan etkileri kapsadığını sınar.
//
// ⛔ Neden gerekli: `config.readme.md` §2.1 "izin kuralının tam metni"
// şunu iddia ediyor:
//
//	"ne cevap gider, ne ajan çağrır, ne hata yazılır, ne 'çıktı boş'
//	 bildirimi gider — ve mesaj OKUNDU da işaretlenmez."
//
// İlk dördü `TestKapaliHatHicbirSeyGondermez` ile yıllardır sınanıyor.
// Beşincisi — mavi tik — hiçbir testte yoktu. Kod okunduğunda doğru
// görünüyordu (`sendSeenIfAsked`, yön seçildikten SONRA çağrılıyor ve
// yön yoksa akış oradan çıkıyor) ama "kod okununca doğru" ile "sınanmış
// doğru" aynı şey değildir. İddia dokümana yazıldıysa iddianın sahibi
// olmak, testini de yazmayı gerektirir.
//
// Bu test olmasa ne olur: birisi günün birinde `sendSeenIfAsked`'ı yön
// seçiminden ÖNCE taşısa, kapalı hatta gelen mesaj mavi tik alır, kullanıcı
// "ben bunu kapatmıştım" der ve hiçbir test kırmızıya döner. Doküman
// sessizce yalan söyler.
func TestKapaliHatMaviTikDeBasmaz(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("cevap"))
	act.AllowGorsel = false // kapalı hat
	evet := true
	act.SendSeen = &evet // ama mavi tik açık
	act.Reply = replyTarget{On: true}

	p := testPayload()
	p.Trigger.Type = "image" // kapalı hatta düşüyor

	fake := &fakeBridge{}
	yanit, err := runAction(t.Context(), opts, act, p, fake.start(t), false, false)
	if err != nil {
		t.Fatalf("kapalı hat hata vermemeli: %v", err)
	}
	if yanit != "" {
		t.Errorf("yanıt = %q, want boş", yanit)
	}
	if len(fake.sent) != 0 {
		t.Errorf("kapalı hatta cevap gitmemeli: %+v", fake.sent)
	}
	if len(fake.isaret) != 0 {
		t.Errorf(
			"kapalı hatta mavi tik BASILMAMALI — okundu işaretleme %d kez çağrıldı: %+v",
			len(fake.isaret), fake.isaret,
		)
	}
}

// TestAcikHattaMaviTikBasilir, yukarıdaki testin sahte-negatif olmasını
// engeller: mavi tik mekanizması zaten kırıksa "kapalı hatta basılmıyor"
// testi de kendiliğinden geçerdi.
//
// İki test birlikte okunmalı: biri bastırmamasını, diğeri bastırabilmesini
// söyler. İkisinden biri olmadan iddianın yarısı sınanmamış sayılır.
func TestAcikHattaMaviTikBasilir(t *testing.T) {
	act, opts := acıkKular(t, jsonMetin("cevap"))
	evet := true
	act.SendSeen = &evet
	act.Reply = replyTarget{On: true}

	p := testPayload()
	p.Trigger.Type = "image"

	fake := &fakeBridge{}
	if _, err := runAction(t.Context(), opts, act, p, fake.start(t), false, false); err != nil {
		t.Fatal(err)
	}
	if len(fake.isaret) != 1 {
		t.Errorf("açık hatta mavi tik 1 kez basılmalı, %d kez basıldı: %+v",
			len(fake.isaret), fake.isaret)
	}
}
