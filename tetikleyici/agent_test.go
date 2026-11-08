// whatsapp-bridge-project/tetikleyici/agent_test.go
//
// Çift gönderim koruması: yanıt veren her akış, gönderim aracı reddeden bir
// ajanla açılmalıdır.
//
// Ölçülen olgu (docs/KRITIK-SORUNLAR-2026-10-04.md #1): ajan `whatsapp_send`
// çağırıyor VE tetikleyici de stdout'u gönderiyordu; kişi aynı şeyi iki kez
// alıyordu. Mefamex 22:05'te sordu: "masha'ya niye 2 kere mesaj gonderiyon".
// Ajanın kendi teşhisi 22:25: "iki kanal çakışıyor".

package main

import (
	"strings"
	"testing"
)

// --agent, oturum eylemine aktarılmalı: gönderim aracı ancak böyle reddedilir.
func TestOturumEylemiAgentAdiniGecirir(t *testing.T) {
	act := action{
		Kind:      "session",
		SessionID: "ses_abc",
		Agent:     "wp-misafir",
	}

	args := opencodeArgs(act, "merhaba")

	i := indexOf(args, "--agent")
	if i < 0 {
		t.Fatalf("--agent aktarılmadı: %v", args)
	}
	if args[i+1] != "wp-misafir" {
		t.Errorf("ajan adı = %q, wp-misafir olmalı", args[i+1])
	}
	if indexOf(args, "--session") < 0 {
		t.Errorf("--session kayboldu: %v", args)
	}
	// Prompt her zaman en sonda.
	if args[len(args)-1] != "merhaba" {
		t.Errorf("prompt sonda değil: %v", args)
	}
}

// agent yazılmamışsa --agent ÜRETİLMEZ. Bu bir hata değil: kullanıcı eski
// config'te çalıştırıyor olabilir. Ama bu durumda ajan gönderme aracına
// sahiptir ve çift gönderim riski vardır — uyarı loglanmalı.
func TestAgentYoksaBayrakUretilmez(t *testing.T) {
	act := action{Kind: "session", SessionID: "ses_abc"}

	args := opencodeArgs(act, "merhaba")

	if indexOf(args, "--agent") >= 0 {
		t.Errorf("agent yokken --agent üretildi: %v", args)
	}
}

// new_session da ajan almalı: tetikleyici oturumu oluşturup yanıt gönderiyor,
// çift gönderim riski aynı.
func TestYeniOturumdaAgentGecer(t *testing.T) {
	act := action{Kind: "new_session", Agent: "wp-sahip"}

	args := opencodeArgs(act, "selam")

	i := indexOf(args, "--agent")
	if i < 0 || args[i+1] != "wp-sahip" {
		t.Errorf("new_session --agent taşımadı: %v", args)
	}
	// new_session BİLEREK --session/--continue kullanmaz (yanlış sohbete
	// bağlanmasın diye). Agent eklenmesi bunu bozmamalı.
	if indexOf(args, "--session") >= 0 || indexOf(args, "--continue") >= 0 {
		t.Errorf("new_session oturum bayrağı taşımamalı: %v", args)
	}
}

// Boşluklu ajan adı (config'de yanlış girintileme) bayrak üretmemeli.
func TestBosAjanAdiBayrakUretmez(t *testing.T) {
	act := action{Kind: "session", SessionID: "ses_abc", Agent: "   "}

	if indexOf(opencodeArgs(act, "x"), "--agent") >= 0 {
		t.Error("boş ajan adıyla --agent üretildi")
	}
}

// Gerçek config'te YANIT VEREN her kural bir ajan kullanmalı. Bu test
// regresyonu yakalar: yeni kural eklenirken agent unutulursa çift gönderim
// sessizce başlar.
//
// İstisna: mefamex-cmd kuralı AGENT KULLANMAZ (stdout doğrudan cevaptır).
// Bu kasıtlı tasarım kararidir (TODO.md ÖNCELİK 1).
func TestGelenConfigHerYanitKuralindaAgentVar(t *testing.T) {
	set, err := loadConfig(shippedConfigPath())
	if err != nil {
		t.Fatalf("config okunamadı: %v", err)
	}

	yanitVeren := 0
	for _, r := range set.Rules {
		for _, a := range r.Actions {
			if !a.Reply.On {
				continue
			}
			yanitVeren++
			// İstisna: mefamex-cmd agent kullanmaz (stdout = cevap)
			if r.Name == "mefamex-cmd" {
				continue
			}
			if strings.TrimSpace(a.Agent) == "" {
				t.Errorf("%s: yanıt gönderen eylemde `agent:` yok — "+
					"ajan whatsapp_send çağırırsa mesaj iki kez gider", r.Name)
			}
		}
	}

	if yanitVeren == 0 {
		t.Fatal("config'de yanıt gönderen eylem yok — test geçersiz")
	}
}

func indexOf(list []string, aranan string) int {
	for i, v := range list {
		if v == aranan {
			return i
		}
	}
	return -1
}
