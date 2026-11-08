package main

import (
	"encoding/json"
	"gopkg.in/yaml.v3"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeBridge simulates the bridge's HTTP API for testing reply logic without
// sending real WhatsApp messages.
//
// NOT — İKİ UÇ: /send yanında /messages da vardır. Çift gönderim
// bastırması (action.go → gonder) her gönderimden önce geçmişi sorgular;
// yalnız /send'i bilen bir sahte, bu sorguyu da "gönderilen mesaj" sayar ve
// mesaj sayısı testleri yanlış çıkar. Gerçek köprü de iki ucu sunar.
type fakeBridge struct {
	sent     []sentMessage
	gecmis   []message
	isaret   []isaretMessage
	failWith int

	// sendFailKalan: ilk N /send isteği 500 ile reddedilir. Retry testleri
	// için: köprü ayakta ama gönderim geçici başarısız senaryosu.
	sendFailKalan int

	// sayacGoruldu, /messages ucuna giden count değerini izleyebilmek için.
	// Pencere sınırsız olsaydı ajanın istemi şişerdi; bunu ölçmek gerekiyor.
	sayacGoruldu func(c *bridgeClient, jid, count string)
}

type sentMessage struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// isaretMessage, köprüye yapılan okundu işaretleme isteğini tutar.
type isaretMessage struct {
	JID        string   `json:"jid"`
	MessageIDs []string `json:"message_ids"`
}

func (f *fakeBridge) start(t *testing.T) *bridgeClient {
	t.Helper()

	// istem, sayacGoruldu geri çağrısına geçilebilsin diye handler'a önceden
	// bağlanır. Sonradan atama yapılsa el yarışı olurdu.
	istem := &bridgeClient{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.failWith != 0 {
			http.Error(w, "bridge rejected", f.failWith)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Geçmiş ucu: yalnız OKU, hiçbir şey gönderilmiş sayılmaz.
		if strings.HasPrefix(r.URL.Path, "/messages") {
			adet := 12
			if v := r.URL.Query().Get("count"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					adet = n
				}
			}
			if f.sayacGoruldu != nil {
				f.sayacGoruldu(istem, r.URL.Query().Get("jid"), r.URL.Query().Get("count"))
			}
			kopya := make([]message, 0, len(f.gecmis))
			for i := len(f.gecmis) - 1; i >= 0 && len(kopya) < adet; i-- {
				kopya = append(kopya, f.gecmis[i])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"count": len(kopya), "jid": "test@lid", "messages": kopya,
			})
			return
		}

		// Okundu işaretleme ucu: gönderim DEĞİLDİR, mesaj sayısına girmemeli.
		// Bu ayrım yanlış yapılırsa send_seen testleri "bir mesaj daha gitti"
		// diye yanlış kırılır.
		if strings.HasPrefix(r.URL.Path, "/mark-read") {
			var is isaretMessage
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &is); err != nil {
				t.Errorf("mark-read body not JSON: %v", err)
			}
			f.isaret = append(f.isaret, is)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		var msg sentMessage
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Errorf("sent body not JSON: %v", err)
		}
		// Retry testi anahtarı: ilk N gönderim 500 ile reddedilir. Notifier
		// bunu yeniden deneyip sonunda başarmalı (ya da maxRetry'de bırakmalı).
		if f.sendFailKalan > 0 {
			f.sendFailKalan--
			http.Error(w, "send failed", http.StatusInternalServerError)
			return
		}
		f.sent = append(f.sent, msg)
		// Gerçek köprüde olduğu gibi: giden mesaj geçmişe de düşer.
		f.gecmis = append(f.gecmis, message{
			Direction: "out", IsFromMe: true,
			From: msg.To, Text: msg.Text,
			Timestamp: time.Now().Unix(),
		})

		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	return newBridgeClient(server.URL, 5*time.Second)
}

func replyOptions() options {
	opts := testOptions()
	opts.sendTimeout = 5 * time.Second
	return opts
}

func TestReplySendsCommandOutputToTheTriggeringChat(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "echo 'hello, this is a reply'"}
	act.Reply = replyTarget{On: true}

	reply, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false)
	if err != nil {
		t.Fatalf("runAction: %v", err)
	}

	if len(fake.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(fake.sent))
	}
	if fake.sent[0].To != "12345678901234@lid" {
		t.Errorf("to = %q, want trigger chat", fake.sent[0].To)
	}
	if fake.sent[0].Text != "hello, this is a reply" {
		t.Errorf("text = %q", fake.sent[0].Text)
	}
	if !strings.Contains(reply, "hello") {
		t.Errorf("returned reply = %q", reply)
	}
}

func TestReplyTrimsSurroundingWhitespace(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "printf '\\n\\n  reply  \\n\\n'"}
	act.Reply = replyTarget{On: true}

	if _, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false); err != nil {
		t.Fatal(err)
	}

	if fake.sent[0].Text != "reply" {
		t.Errorf("text = %q, whitespace should be trimmed", fake.sent[0].Text)
	}
}

func TestFixedReplyTextOverridesCommandOutput(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "echo 'this output should be ignored'"}
	act.Reply = replyTarget{On: true, Text: "fixed reply"}

	if _, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false); err != nil {
		t.Fatal(err)
	}

	if fake.sent[0].Text != "fixed reply" {
		t.Errorf("text = %q, fixed text should be used", fake.sent[0].Text)
	}
}

func TestNoReplyWhenNotRequested(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "echo 'should not be sent'"}

	reply, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.sent) != 0 {
		t.Errorf("no reply requested, but %d sent", len(fake.sent))
	}
	if reply != "" {
		t.Errorf("reply should be empty, got %q", reply)
	}
}

// ÇIKTI BOŞSA KULLANICIYA BİLDİRİM GÖNDERİLİR.
//
// Bu davranış 2026-10-04'te BİLİNÇLİ OLARAK DEĞİŞTİRİLDİ. Önceden boş
// çıktıda hiçbir şey gönderilmiyordu; ölçüm: 124 tetiklemenin 25'i (%20)
// bu yolda sessizce düştü, kullanıcı "cevap vermiyor musun" diye soruyordu
// ve sebep hiçbir yerde görünmüyordu. Ayrıntı: docs/KRITIK-SORUNLAR-2026-10-04.md
// #6. Ayrıntılı davranış teslim_test.go'da.
func TestReplyNotifiesUserWhenOutputIsEmpty(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "true"}
	act.Reply = replyTarget{On: true}

	_, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false)
	if err == nil {
		t.Fatal("boş çıktı hata vermeli")
	}

	if len(fake.sent) != 1 {
		t.Fatalf("kullanıcıya 1 bildirim gitmeliydi, %d gitti", len(fake.sent))
	}
	if strings.TrimSpace(fake.sent[0].Text) == "" {
		t.Error("bildirim boş olmamalı")
	}
	// Bildirim kısa ve iç bilgi sızdırmayan olmalı.
	if len(fake.sent[0].Text) > 160 {
		t.Errorf("bildirim çok uzun: %q", fake.sent[0].Text)
	}
}

func TestReplyStillGoesOutWhenCommandExitsNonZero(t *testing.T) {
	fake := &fakeBridge{}
	client := fake.start(t)

	act := action{Kind: "command", Code: "echo 'useful reply'; exit 1"}
	act.Reply = replyTarget{On: true}

	reply, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false)
	if err != nil {
		t.Fatalf("runAction: %v", err)
	}
	if len(fake.sent) != 1 || fake.sent[0].Text != "useful reply" {
		t.Errorf("output should be sent even on non-zero exit: %+v", fake.sent)
	}
	if !strings.Contains(reply, "useful") {
		t.Errorf("reply = %q", reply)
	}
}

func TestReplyReportsBridgeRejection(t *testing.T) {
	fake := &fakeBridge{failWith: http.StatusInternalServerError}
	client := fake.start(t)

	act := action{Kind: "command", Code: "echo reply"}
	act.Reply = replyTarget{On: true}

	_, err := runAction(t.Context(), replyOptions(), act, testPayload(), client, false, false)
	if err == nil {
		t.Fatal("bridge rejection should error")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error should indicate rejection, got: %v", err)
	}
}

func TestReplyTargetAcceptsTrueOrText(t *testing.T) {
	cases := []struct {
		raw      string
		wantOn   bool
		wantText string
	}{
		{`true`, true, ""},
		{`false`, false, ""},
		{`"fixed reply"`, true, "fixed reply"},
	}

	for _, tc := range cases {
		var got replyTarget
		if err := yaml.Unmarshal([]byte(tc.raw), &got); err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if got.On != tc.wantOn || got.Text != tc.wantText {
			t.Errorf("%s -> %+v, want on=%v text=%q", tc.raw, got, tc.wantOn, tc.wantText)
		}
	}
}

func TestReplyTargetRejectsNonsense(t *testing.T) {
	var got replyTarget
	if err := yaml.Unmarshal([]byte(`42`), &got); err == nil {
		t.Error("integer should be rejected")
	}
}

func TestActionLabelShowsTheReply(t *testing.T) {
	act := action{Kind: "command", Code: "echo x"}
	if strings.Contains(act.label(), "reply") {
		t.Errorf("no reply -> label should not mention it: %q", act.label())
	}

	act.Reply = replyTarget{On: true}
	if !strings.Contains(act.label(), "reply") {
		t.Errorf("reply -> label should mention it: %q", act.label())
	}
}

func TestReplyFromConfigWorksEndToEnd(t *testing.T) {
	set, err := parseConfig([]byte(`rules:
- name: with_reply
  match: { name: "Mefamex" }
  do: [ { type: command, code: "echo reply", reply: true } ]`))
	if err != nil {
		t.Fatal(err)
	}

	act := set.Rules[0].Actions[0]
	if !act.Reply.On {
		t.Fatal("config `reply: true` should be parsed")
	}
}
