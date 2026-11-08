package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testPayload() payload {
	trigger := mefamexMessage()
	return payload{
		Trigger: trigger,
		History: []message{trigger},
	}
}

func testOptions() options {
	return options{actionTimeout: 10 * time.Second, fetchTimeout: time.Second, probeTimeout: time.Second}
}

func TestCommandActionPassesTriggerThroughEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "rapor.txt")
	scriptPath := filepath.Join(dir, "s.sh")

	script := "#!/bin/sh\n" +
		"{\n" +
		"  echo \"ENV_NAME=$TRIGGER_NAME\"\n" +
		"  echo \"ENV_JID=$TRIGGER_JID\"\n" +
		"  echo \"ENV_TEXT=$TRIGGER_TEXT\"\n" +
		"  echo \"ENV_MESSAGES=$TRIGGER_MESSAGES\"\n" +
		"  echo \"STDIN:$(cat)\"\n" +
		"} > " + report + "\n"

	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := runAction(t.Context(), testOptions(), action{Kind: "command", Code: scriptPath}, testPayload(), nil, false, false)
	if err != nil {
		t.Fatalf("runAction: %v", err)
	}

	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)

	for _, want := range []string{
		"ENV_NAME=Mefamex",
		"ENV_JID=12345678901234@lid",
		"ENV_TEXT=Müfredat nedir?",
		"ENV_MESSAGES=Mefamex: Müfredat nedir?",
		"STDIN:Müfredat nedir?",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
}

func TestInlineCommandExpandsThePlaceholder(t *testing.T) {
	code := "printf '%s' {mesajlar} > " + filepath.Join(t.TempDir(), "cikti.txt")

	got := expandPlaceholder(code, "Mefamex: merhaba")

	if !strings.Contains(got, "printf '%s' 'Mefamex: merhaba' >") {
		t.Errorf("placeholder should be quoted and expanded, got: %s", got)
	}
}

func TestPlaceholderIsLeftAloneWhenAbsent(t *testing.T) {
	if got := expandPlaceholder("echo hello", "context"); got != "echo hello" {
		t.Errorf("no placeholder -> command unchanged, got %q", got)
	}
}

func TestSessionActionPinsTheSession(t *testing.T) {
	args := opencodeArgs(action{Kind: "session", SessionID: "ses_abc"}, "hello")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--session ses_abc") {
		t.Errorf("session should be pinned, got: %s", joined)
	}
	if strings.Contains(joined, "--continue") {
		t.Errorf("--continue must not be used, got: %s", joined)
	}
}

func TestNewSessionActionPinsNothing(t *testing.T) {
	args := opencodeArgs(action{Kind: "new_session"}, "hello")
	joined := strings.Join(args, " ")

	for _, forbidden := range []string{"--session", "--continue", "-c "} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("new_session must not use %s, got: %s", forbidden, joined)
		}
	}
}

func TestOpencodeArgsAlwaysEndsWithThePrompt(t *testing.T) {
	for _, kind := range []string{"session", "new_session"} {
		args := opencodeArgs(action{Kind: kind, SessionID: "ses_x"}, "PROMPT CONTENT")
		if args[len(args)-1] != "PROMPT CONTENT" {
			t.Errorf("%s: prompt must be last arg, got %v", kind, args)
		}
	}
}

func TestOpencodeActionReportsMissingBinary(t *testing.T) {
	opts := testOptions()
	opts.opencode = "/nonexistent/opencode"

	_, err := runAction(t.Context(), opts, action{Kind: "new_session"}, testPayload(), nil, false, false)
	if err == nil {
		t.Fatal("missing binary should error")
	}
	if !strings.Contains(err.Error(), "opencode") {
		t.Errorf("error should blame opencode, got: %v", err)
	}
}

func TestOpencodeActionReportsUnsetPath(t *testing.T) {
	_, err := runAction(t.Context(), testOptions(), action{Kind: "session", SessionID: "ses_x"}, testPayload(), nil, false, false)
	if err == nil || !strings.Contains(err.Error(), "opencode") {
		t.Errorf("unset path should error, got: %v", err)
	}
}

func TestUnknownActionKindFails(t *testing.T) {
	_, err := runAction(t.Context(), testOptions(), action{Kind: "unknown"}, testPayload(), nil, false, false)
	if err == nil {
		t.Fatal("unknown action kind should error")
	}
}

func TestCommandTimeoutIsEnforced(t *testing.T) {
	opts := testOptions()
	opts.actionTimeout = 200 * time.Millisecond

	start := time.Now()
	_, err := runAction(t.Context(), opts, action{Kind: "command", Code: "sleep 5"}, testPayload(), nil, false, false)

	if err == nil {
		t.Fatal("long command should timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout not enforced, %s elapsed", elapsed)
	}
}

func TestPayloadEnvCarriesEveryTriggerField(t *testing.T) {
	env := testPayload().env()
	joined := strings.Join(env, "\n")

	for _, want := range []string{
		"TRIGGER_TEXT=",
		"TRIGGER_NAME=Mefamex",
		"TRIGGER_JID=12345678901234@lid",
		"TRIGGER_ID=W1",
		"TRIGGER_MESSAGES=",
		"TRIGGER_JSON=",
		"TRIGGER_CONFIG=",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %q:\n%s", want, joined)
		}
	}
}

func TestBuildPromptIncludesConversation(t *testing.T) {
	got := buildPrompt(testPayload())

	for _, want := range []string{"Mefamex", "12345678901234@lid", "Müfredat nedir?"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}

// ══ Rate limit sonrası otomatik devam (2026-10-06) ══════════════════════
//
// Sahibin isteği: "rate limitte direkt continue diye mesaj gondersin".
// Model kota hatası verince aynı oturuma "Continue, unstop" otomatik verilir.
// Aşağıdaki testler TÜM akışı sahte opencode ile ölçer: ilk N çağrı stderr'e
// "Rate limit" basıp exit 1, sonrası JSON metin olayı basıp exit 0.

// kotaliOpencode, çağrı sayacına göre davranan sahte opencode üretir.
//
// İlk ilkKotali çağrı hataMetni'ni stderr'e basıp exit 1 döner (model
// hatasını simüle eder — varsayılan olarak "Rate limit exceeded (429)").
// Sonraki çağrılar cevapJSON'u stdout'a basıp exit 0 döner. Her çağrı bir
// sonraki son-çağrının argümanlarını ARGS dosyasına (NUL ayraçlı) yazar —
// devam prompt'unun geçtiğini kanıtlamak için.
//
// ⛔ SAYAÇ AYRI DOSYADA (bekleme_akis_test.go'da öğrenilen tuzak): sayaç
// betiğin kendi kaynak metnini saymamalı.
func kotaliOpencode(t *testing.T, ilkKotali int, cevapJSON string) (betik, sayac, argsDosya string) {
	return kotaliOpencodeHata(t, ilkKotali, cevapJSON, "Rate limit exceeded for model (429)")
}

// kotaliOpencodeHata, hataMetni'ni özelleştirmek isteyen testler için.
func kotaliOpencodeHata(t *testing.T, ilkKotali int, cevapJSON, hataMetni string) (betik, sayac, argsDosya string) {
	t.Helper()

	dizin := t.TempDir()
	sayac = filepath.Join(dizin, "kez.txt")
	argsDosya = filepath.Join(dizin, "son-args.txt")
	betik = filepath.Join(dizin, "kotali-opencode.sh")

	// Değerleri sh betiğine TEK TIRNAKLA gömeriz (2026-10-06: "Rate limit
	// exceeded for model (429)" tırnaksız gömülünce parantez/boşluk betiği
	// sözdizimi hatasıyla kırmıştı). Tek tırnak içindeki tek tırnaklar
	// '\'' ile kaçırılır.
	shTirnak := func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}

	icerik := fmt.Sprintf("#!/bin/sh\n"+
		"SAYAC='%s'\n"+
		"ARGS='%s'\n"+
		"ILK=%d\n"+
		"HATA=%s\n"+
		"CEVAP=%s\n"+
		"n=0\n"+
		"[ -f \"$SAYAC\" ] && n=$(cat \"$SAYAC\" 2>/dev/null || echo 0)\n"+
		"n=$((n+1))\n"+
		"echo \"$n\" > \"$SAYAC\"\n"+
		"printf '%%s\\0' \"$@\" > \"$ARGS\"\n"+
		"if [ \"$n\" -le \"$ILK\" ]; then\n"+
		"  echo \"$HATA\" >&2\n"+
		"  exit 1\n"+
		"fi\n"+
		"printf '%%s\\n' \"$CEVAP\"\n"+
		"exit 0\n",
		sayac, argsDosya, ilkKotali, shTirnak(hataMetni), shTirnak(cevapJSON))

	if err := os.WriteFile(betik, []byte(icerik), 0o700); err != nil {
		t.Fatal(err)
	}
	return betik, sayac, argsDosya
}

// sayacOkuSayi, sayaç dosyasındaki sayıyı okur (kotaliOpencode biçimi).
func sayacOkuSayi(t *testing.T, sayac string) int {
	t.Helper()
	ham, err := os.ReadFile(sayac)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(ham)))
	if err != nil {
		t.Fatalf("sayaç = %q, sayı değil: %v", ham, err)
	}
	return n
}

// sonArgumanlar, son çağrının NUL ayraçlı argümanlarını okur.
func sonArgumanlar(t *testing.T, argsDosya string) []string {
	t.Helper()
	ham, err := os.ReadFile(argsDosya)
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

func devamEylemi() action {
	return action{Kind: "session", SessionID: "ses_x", Reply: replyTarget{On: true}}
}

// devamOptions, otomatik devam testleri için: devamAra 0 (denemeler arası
// bekleme yok) ve gönderim zaman aşımı dolu. testOptions'ta sendTimeout 0'dır;
// 0 → context.WithTimeout(..., 0) anında dolar → "gönderilemedi" hatasıyla
// devam testleri YALNIZCA bu yüzden kırılırdı.
func devamOptions() options {
	opts := testOptions()
	opts.sendTimeout = 5 * time.Second
	return opts
}

// ⛔ ANAHTAR TEST: ilk çağrı kota verir, otomatik devam oturumu sürdürür ve
// cevabı KULLANICIYA devam cevabı olarak ulaştırır. "Kota doldu" bildirimi
// GİTMEMELİ — sahibin şikâyeti tam buydu: iş yarım kalıyordu.
func TestKotaSonrasiOtomatikDevamOturumuSurdurur(t *testing.T) {
	betik, sayac, argsDosya := kotaliOpencode(t, 1,
		`{"type":"text","part":{"type":"text","text":"Devam sonucu"}}`)

	opts := devamOptions() // devamAra 0 → denemeler arası bekleme yok
	opts.opencode = betik

	fake := &fakeBridge{}
	yanit, err := runAction(t.Context(), opts, devamEylemi(), testPayload(), fake.start(t), false, false)
	if err != nil {
		t.Fatalf("otomatik devam sonrası hata olmamalı: %v", err)
	}

	if !strings.Contains(yanit, "Devam sonucu") {
		t.Errorf("cevap = %q, devam sonucunu içermeli", yanit)
	}
	if kez := sayacOkuSayi(t, sayac); kez != 2 {
		t.Errorf("opencode %d kez çalıştı, want 2 (ilk kotalı + devam)", kez)
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0].Text, "Devam sonucu") {
		t.Errorf("gönderilenler = %+v, want YALNIZ devam cevabı", fake.sent)
	}

	// Devam çağrısı aynı oturuma bağlı ve argüman SONDAN devam imleci.
	args := sonArgumanlar(t, argsDosya)
	if !icerirMi(args, "--session") || !icerirMi(args, "ses_x") {
		t.Errorf("devam aynı oturuma gitmeli: %v", args)
	}
	if args[len(args)-1] != devamPrompt {
		t.Errorf("son argüman devam imleci olmalı, got %q", args[len(args)-1])
	}
}

// Devam imleci kullanıcıya GÖRÜNMEMELİ: "Continue, unstop" sohbete yazılmaz,
// oturumun kendisine verilir.
func TestOtomatikDevamImleciKullaniciyaGitmez(t *testing.T) {
	betik, _, _ := kotaliOpencode(t, 1,
		`{"type":"text","part":{"type":"text","text":"Devam sonucu"}}`)
	opts := devamOptions()
	opts.opencode = betik

	fake := &fakeBridge{}
	_, err := runAction(t.Context(), opts, devamEylemi(), testPayload(), fake.start(t), false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fake.sent {
		if strings.Contains(m.Text, "Continue, unstop") {
			t.Errorf("devam imleci kullanıcıya gitmemeli: %q", m.Text)
		}
	}
}

// Kota HİÇ toparlanmazsa: 2 sessiz devam denemesinden sonra hazır yolda
// yalnız TEK bildirim gider ("kota doldu") ve hata döner — bekleme koruması
// (engine) devreye girebilsin. Başarısız turların çıktısı sohbete düşmez.
func TestKotaHicToparlanmazsaTekBildirimVeHata(t *testing.T) {
	betik, sayac, _ := kotaliOpencode(t, 99,
		`{"type":"text","part":{"type":"text","text":"x"}}`)
	opts := devamOptions()
	opts.opencode = betik

	fake := &fakeBridge{}
	_, err := runAction(t.Context(), opts, devamEylemi(), testPayload(), fake.start(t), false, false)
	if err == nil {
		t.Fatal("tüm denemeler kotalıyken hata dönmeli (engine bekleme kursun)")
	}
	if kez := sayacOkuSayi(t, sayac); kez != 3 {
		t.Errorf("opencode %d kez çalıştı, want 3 (ilk + 2 sessiz devam)", kez)
	}
	if len(fake.sent) != 1 || !strings.Contains(fake.sent[0].Text, "yük altında") {
		t.Errorf("kullanıcı TEK kota bildirimi almalı, got %+v", fake.sent)
	}
}

// SessizHata turları (otomatik devamın her denemesi) kullanıcıya HİÇBİR ŞEY
// göndermemeli: ne kota bildirimi, ne kısmi çıktı. Sonuçta ana akış söyler.
func TestSessizHataTuruKullaniciyaHicGitmez(t *testing.T) {
	betik, _, _ := kotaliOpencode(t, 99,
		`{"type":"text","part":{"type":"text","text":"x"}}`)
	opts := testOptions()
	opts.opencode = betik

	fake := &fakeBridge{}
	act := devamEylemi()
	act.SessizHata = true
	_, err := runAction(t.Context(), opts, act, testPayload(), fake.start(t), false, false)
	if err == nil {
		t.Fatal("sessiz tur kotalıyken hata dönmeli")
	}
	if len(fake.sent) != 0 {
		t.Errorf("sessiz tur hiçbir şey göndermemeli: %+v", fake.sent)
	}
}

// Rate limit DIŞI hata (örneğin açıkça başka bir model hatası) devamı
// TETİKLEMEMELİ: "Continue, unstop" kotayı değil, yarım kalan işi geri
// ister. Kotalı olmayan hata tek çağrıda kalmalı; kullanıcı tek bildirim
// alır ve devam imleci oturuma verilmez.
func TestKotaliOlmayanHataDevamTetiklemez(t *testing.T) {
	betik, sayac, argsDosya := kotaliOpencodeHata(t, 1,
		`{"type":"text","part":{"type":"text","text":"x"}}`,
		"JSON ayrıştırılamadı, yanıt bozuk")

	opts := devamOptions()
	opts.opencode = betik

	fake := &fakeBridge{}
	_, err := runAction(t.Context(), opts, devamEylemi(), testPayload(), fake.start(t), false, false)
	if err == nil {
		t.Fatal("model hatası hata dönmeli")
	}
	if kez := sayacOkuSayi(t, sayac); kez != 1 {
		t.Errorf("opencode %d kez çalıştı, want 1 (devam kotalı olmayan hatada GİTMEMELİ)", kez)
	}
	// Devam imleci hiçbir çağrıya prompt olarak verilmemeli — tek çağrı
	// zaten açık promptla gitti.
	args := sonArgumanlar(t, argsDosya)
	if args[len(args)-1] == devamPrompt {
		t.Errorf("kotalı olmayan hatada devam imleci kullanılmamalı: %v", args)
	}
	// Kullanıcı yine de tek bildirim alır (sessizlik yasak).
	if len(fake.sent) != 1 {
		t.Errorf("kullanıcı tek bildirim almalı, got %+v", fake.sent)
	}
}
