package main

// DİZİN DENETİMİ — tetikleyici.yaml gerçekten yükleniyor mu, yönlendirme
// beklenen hatta gidiyor mu, betik yolları dosya sisteminde var mı?
//
// Bu test config dosyasının kendisine bakar (test içinde uydurma YAML değil).
// Neden: 6 kurallık düzende YAML'daki bir girinti hatası tetikleyiciyi
// sessizce eski kurallarla çalıştırıyordu; kural sayısını görmek yetmiyordu,
// GERÇEKTEN yeni düzeni yüklendiğini görmek gerekiyordu.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func yukleGercekConfig(t *testing.T) ruleSet {
	t.Helper()
	ham, err := os.ReadFile("../tetikleyici.yaml")
	if err != nil {
		t.Fatalf("tetikleyici.yaml okunamadı: %v", err)
	}
	set, err := parseConfig(ham)
	if err != nil {
		t.Fatalf("tetikleyici.yaml PARSE EDİLMEDİ: %v", err)
	}
	return set
}

// Altı kural tek kurala indi. Bu, sahnenin sahibinin isteğiydi:
// aynı numara için 6 ayrı kural yazmak hem eşleşmeyi hem bakımı zorlaştırıyordu.
func TestGercekConfigTekKuralIceriyor(t *testing.T) {
	set := yukleGercekConfig(t)

	if len(set.Rules) != 1 {
		var adlar []string
		for _, r := range set.Rules {
			adlar = append(adlar, r.Name)
		}
		t.Fatalf("kural sayısı = %d (%v), want 1", len(set.Rules), adlar)
	}
	if set.Rules[0].Name != "mefamex" {
		t.Errorf("kural adı = %q, want mefamex", set.Rules[0].Name)
	}
}

// wp_number/wp_name gerçek dosyadan okunup number/name'e birleşmeli.
func TestGercekConfigKimlikDogruEslesiyor(t *testing.T) {
	set := yukleGercekConfig(t)
	kural := set.Rules[0]

	// LID ve telefon İKİSİ de yazılı olmalı: LID türetilemez.
	var telefon, lid bool
	for _, n := range kural.Match.Numbers {
		switch {
		case strings.HasSuffix(n, "@lid"):
			lid = true
		case !strings.Contains(n, "@"):
			telefon = true
		}
	}
	if !telefon || !lid {
		t.Errorf("numaralar = %v — telefon ve LID ikisi de gerekir", kural.Match.Numbers)
	}
	if len(kural.Match.Names) != 1 || kural.Match.Names[0] != "Mefamex" {
		t.Errorf("isimler = %v", kural.Match.Names)
	}

	// Pasif misafirler (Burak/Yusuf/Masha) kural listesinde OLMAMALI.
	for _, r := range set.Rules {
		for _, n := range r.Match.Numbers {
			if strings.Contains(n, "905550000002") || strings.Contains(n, "905550000003") ||
				strings.Contains(n, "905550000004") {
				t.Errorf("pasif misafir kuralı etkinleşmiş: %s", r.Name)
			}
		}
	}
}

func TestGercekConfigOpencodeEylemiTamYetenekli(t *testing.T) {
	set := yukleGercekConfig(t)
	act := set.Rules[0].Actions[0]

	if act.Kind != "opencode" {
		t.Fatalf("eylem tipi = %q, want opencode", act.Kind)
	}
	if act.SessionID == "" {
		t.Error("session boş")
	}
	if act.Agent != "wp-sahip" {
		t.Errorf("agent = %q — sahibin oturumu olmalı (çift gönderim)", act.Agent)
	}

	// Kapalı kalmaması gereken yetenekler.
	for _, ad := range []string{
		"allow_help", "allow_cmd", "allow_durum", "allow_camera",
		"allow_gorsel", "allow_ses", "allow_belge", "allow_reaction",
	} {
		_ = ad
	}
	if !act.AllowHelp || !act.AllowCmd || !act.AllowDurum || !act.AllowCamera ||
		!act.AllowGorsel || !act.AllowSes || !act.AllowBelge || !act.AllowReaction {
		t.Errorf("bütün yetenekler açık olmalı: %s", act.aciklama())
	}

	if act.SendSeen == nil || !*act.SendSeen {
		t.Error("send_seen: true olmalı")
	}
	if act.AddAITag == nil || *act.AddAITag {
		t.Error("add_aitag: false olmalı (global true olsa bile geçersiz kılmalı)")
	}
	if act.DoDebug == nil || *act.DoDebug {
		t.Error("do_debug: false olmalı")
	}
}

// YAML katlama (">-") betik yollarını bozmamalı: tek satır, tırafsız.
// Çok satırlı olsaydı komut satırı "path\n  argüman" olarak birleşir ve
// shell "command not found" verirdi.
func TestGercekConfigBetikYollariTekSatir(t *testing.T) {
	set := yukleGercekConfig(t)
	act := set.Rules[0].Actions[0]

	for _, alan := range []struct{ ad, yol string }{
		{"cmd", act.Cmd},
		{"gorsel", act.Gorsel},
		{"ses", act.Ses},
	} {
		if strings.TrimSpace(alan.yol) == "" {
			t.Errorf("%s yolu boş", alan.ad)
			continue
		}
		if strings.ContainsAny(alan.yol, "\n\r") {
			t.Errorf("%s yolu çok satırlı: %q", alan.ad, alan.yol)
		}
		if strings.HasSuffix(alan.yol, " ") {
			t.Errorf("%s yolu sonda boşluk bırakmış: %q", alan.ad, alan.yol)
		}
	}
}

// Yazılan betikler gerçekten var mı? Yoksa hat kurulu ama çalışmıyor demektir
// ve kullanıcı "komutlar çalışmıyor" diye arar.
func TestGercekConfigBetiklerDiskteVar(t *testing.T) {
	set := yukleGercekConfig(t)
	act := set.Rules[0].Actions[0]

	for _, alan := range []struct{ ad, yol string }{
		{"cmd", act.Cmd},
		{"gorsel", act.Gorsel},
		{"ses", act.Ses},
	} {
		genislet := expandHome(alan.yol)
		if _, err := os.Stat(genislet); err != nil {
			if _, err2 := os.Stat(filepath.Join("..", genislet)); err2 == nil {
				continue
			}
			kirpilmis := strings.TrimPrefix(genislet, "tetikleyici/")
			if _, err3 := os.Stat(kirpilmis); err3 == nil {
				continue
			}
			t.Errorf("%s betiği yok: %s (%v)", alan.ad, genislet, err)
		}
	}
}

// opencode binary'si config'de tanımlı ve erişilebilir mi?
func TestGercekConfigOpencodeYoluVar(t *testing.T) {
	ham, err := os.ReadFile("../tetikleyici.yaml")
	if err != nil {
		t.Fatal(err)
	}
	set, err := parseConfig(ham)
	if err != nil {
		t.Fatal(err)
	}
	// opencode yolu options'a taşınır; config'de tanımlı olması şart.
	if !strings.Contains(string(ham), "opencode:") {
		t.Error("config'de opencode yolu tanımlı değil")
	}
	_ = set
}

// `ping` kuralı SİLİNMİŞ olmalı — sahibi "/help zaten aynı işlevi görüyor" dedi.
func TestGercekConfigPingKuraliSilinmis(t *testing.T) {
	set := yukleGercekConfig(t)
	for _, r := range set.Rules {
		if strings.Contains(strings.ToLower(r.Name), "ping") {
			t.Errorf("ping kuralı hâlâ var: %s", r.Name)
		}
		if strings.Contains(strings.ToLower(r.Match.NotText), "ping") {
			t.Errorf("not_text: ping hâlâ var: %s", r.Name)
		}
	}
}

// Betik yolları proje dizini içinde mi? Dışarıdaki bir yol, projeyi taşınınca
// sessizce ölür.
func TestBetiklerProjeDiziniIcinde(t *testing.T) {
	set := yukleGercekConfig(t)
	act := set.Rules[0].Actions[0]
	kok, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	for _, alan := range []struct{ ad, yol string }{
		{"cmd", act.Cmd},
		{"gorsel", act.Gorsel},
		{"ses", act.Ses},
	} {
		mutlak, err := filepath.Abs(expandHome(alan.yol))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(mutlak, kok+string(filepath.Separator)) {
			t.Errorf("%s proje dışında: %s", alan.ad, mutlak)
		}
	}
}
