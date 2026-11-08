package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shippedConfigPath returns the path to the config file at project root.
func shippedConfigPath() string {
	p := filepath.Join("..", "tetikleyici.yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join("..", "tetikleyici.example.yaml")
}

// shippedDocsPath returns the companion documentation file. The config is kept
// short on purpose (2026-10-06: 410 satırın 348'i yorumdu); the option
// reference, the worked examples and the environment-variable table live here.
func shippedDocsPath() string {
	return filepath.Join("..", "config.readme.md")
}

func readShippedConfig(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile(shippedConfigPath())
	if err != nil {
		t.Fatalf("config not found: %v", err)
	}
	return string(raw)
}

// readShippedDocs returns the config and its companion documentation joined.
// Documentation assertions read this, not the config alone: the config is
// deliberately lean, so demanding every example fit inside it is what made it
// balloon in the first place.
func readShippedDocs(t *testing.T) string {
	t.Helper()

	docs, err := os.ReadFile(shippedDocsPath())
	if err != nil {
		t.Fatalf("config.readme.md not found: %v", err)
	}
	return readShippedConfig(t) + "\n" + string(docs)
}

func TestShippedConfigPointsAtItsDocumentation(t *testing.T) {
	raw := readShippedConfig(t)

	// Config kısa tutulduğu için ayrıntı başka bir dosyada. Bağlantı koparsa
	// o ayrıntı kimseye ulaşamaz — o yüzden burada doğrulanır.
	if !strings.Contains(raw, "config.readme.md") {
		t.Error("config must point at config.readme.md")
	}
}

func TestShippedConfigStaysLean(t *testing.T) {
	// 2026-10-06: 410 satırın 348'i yorumdu, gerçek veri 48 satırdı. Aynı
	// şişkinlik geri gelirse bu test sessizce geçmesin.
	// 2026-10-06 (cmd eklendi): ~251 yorum satırı → sınır 280'a çıkarıldı.
	raw := readShippedConfig(t)

	var commentLines int
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			commentLines++
		}
	}

	if commentLines > 280 {
		t.Errorf("config şişiyor: %d yorum satırı (sınır 280). Açıklamalar config.readme.md'ye taşınsın", commentLines)
	}
}

func TestShippedConfigParses(t *testing.T) {
	set, err := loadConfig(shippedConfigPath())
	if err != nil {
		t.Fatalf("shipped config unreadable: %v", err)
	}
	if len(set.Rules) == 0 {
		t.Fatal("config must have at least one rule")
	}
	if set.OpenCode == "" {
		t.Error("opencode path must be defined")
	}
}

func TestShippedConfigRulesAreRunnable(t *testing.T) {
	set, err := loadConfig(shippedConfigPath())
	if err != nil {
		t.Fatal(err)
	}

	// Mefamex'in yazdığı bir mesaj oturum kuralına düşmeli.
	hit := message{
		ID:        "W",
		Type:      "text",
		From:      "12345678901234@lid",
		Sender:    "12345678901234@lid",
		Text:      "merhaba",
		PushName:  "Mefamex",
		Direction: "in",
	}

	if len(matchingRules(set, hit)) == 0 {
		t.Error("Mefamex message should match")
	}

	other := message{ID: "X", From: "905999999999@s.whatsapp.net", PushName: "Other", Direction: "in"}
	if len(matchingRules(set, other)) != 0 {
		t.Error("unrelated chat should not match")
	}
}

// Sesli not ajanın oturumuna DÜŞMEMELİ: içi boş olduğu için ajan
// "[audio tipinde mesaj]" görüp anlamsız cevap üretirdi. Tek kural
// düzeninde bu ayrım "hangi hat" kararına taşındı (assistant.go).
func TestShippedConfigKeepsAudioOutOfTheAgentSession(t *testing.T) {
	set, err := loadConfig(shippedConfigPath())
	if err != nil {
		t.Fatal(err)
	}

	voice := message{
		ID: "V", Type: "audio",
		From: "12345678901234@lid", Sender: "12345678901234@lid",
		PushName: "Mefamex", Direction: "in",
	}

	var hitler []rule
	for _, r := range matchingRules(set, voice) {
		hitler = append(hitler, r)
	}

	if len(hitler) != 1 {
		t.Fatalf("sesli not %d kural tetikledi (%v), want 1", len(hitler), ruleAdlari(hitler))
	}

	act := hitler[0].Actions[0]
	if act.Kind != "opencode" {
		t.Fatalf("eylem tipi = %q", act.Kind)
	}

	hat, ok := act.yonlendir(voice)
	if !ok {
		t.Fatal("sesli not yönlendirilmeli")
	}
	if hat.tip != "command" {
		t.Errorf("sesli not hat tipi = %q, want command (ajan oturumu değil)", hat.tip)
	}
	if hat.betik != act.Ses {
		t.Errorf("sesli not %q betiğine gitmeli, %q'ye gitti", act.Ses, hat.betik)
	}
}

// ruleAdlari, eşleşen kuralların adlarını listeler — hata mesajlarında okunur.
func ruleAdlari(rules []rule) []string {
	var adlar []string
	for _, r := range rules {
		adlar = append(adlar, r.name())
	}
	return adlar
}

// Pasif misafir kuralları tetikleyici'ye YÜKLENMEZ. Ayrı dosyada dururlar
// (pasif-kurallar.yaml) ve orada gerçekten yazılıdır.
func TestShippedConfigHasNoGuestRules(t *testing.T) {
	raw := readShippedConfig(t)
	for _, yasak := range []string{"burak-oturum", "yusuf-oturum", "masha-oturum",
		"burak-ping", "yusuf-ping", "masha-ping"} {
		if strings.Contains(raw, "\n  - name: "+yasak) {
			t.Errorf("pasif misafir kuralı etkin config'de: %s", yasak)
		}
	}
	if !strings.Contains(raw, "pasif-kurallar.yaml") {
		t.Error("config pasif-kurallar.yaml dosyasına yönlendirmeli")
	}
}

// Pasif dosya ÇÖZÜMLENEBİLİR olmalı: kullanıcı oradan kopyalayıp yapıştıracak.
// Bozuk kalan bir örnek dosya, kuralı etkinleştirme denemesini sessizce
// başarısız yapar.
func TestPasifKurallarDosyasiCozumlenir(t *testing.T) {
	ham, err := os.ReadFile(filepath.Join("..", "pasif-kurallar.yaml"))
	if err != nil {
		t.Fatalf("pasif-kurallar.yaml okunamadı: %v", err)
	}

	set, err := parseConfig(ham)
	if err != nil {
		t.Fatalf("pasif-kurallar.yaml parse edilemedi: %v", err)
	}
	if len(set.Rules) != 6 {
		var adlar []string
		for _, r := range set.Rules {
			adlar = append(adlar, r.name())
		}
		t.Errorf("pasif kural sayısı = %d (%v), want 6", len(set.Rules), adlar)
	}

	// Her eşleşen kural gerçekten çalıştırılabilir olmalı (check() çalıştırılır).
	for _, r := range set.Rules {
		for i, act := range r.Actions {
			if err := act.check(); err != nil {
				t.Errorf("pasif %s eylem %d geçersiz: %v", r.name(), i, err)
			}
		}
	}

	// Misafirler kendi ajanlarını kullanır; sahibin oturumunu (wp-sahip)
	// bir yerde yanlışlıkla paylaşmak sınırları delerdi.
	for _, r := range set.Rules {
		for _, act := range r.Actions {
			if act.Agent == "wp-sahip" {
				t.Errorf("pasif %s kuralı sahibin ajanını (wp-sahip) kullanıyor", r.name())
			}
		}
	}
}

func TestShippedConfigHasTemplateExamples(t *testing.T) {
	raw := readShippedDocs(t)

	// Şablon örnekler config.readme.md'de. config tek kurala indiği için
	// buradaki isimler eski "mefamex-oturum / -sesli-not" değil, güncel
	// tek kural ve dört eylem türü.
	for _, want := range []string{
		`match: { everyone: true }`,
		`- name: mefamex`,
		`type: opencode`,
		`type: command`,
		`type: session`,
		`type: new_session`,
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("shipped docs missing template example %q", want)
		}
	}
}

func TestShippedConfigCoversEveryActionKind(t *testing.T) {
	raw := readShippedConfig(t)

	for _, kind := range []string{"command", "session", "new_session", "opencode"} {
		if !strings.Contains(raw, kind) {
			t.Errorf("shipped config missing %s mention", kind)
		}
	}

	for _, field := range []string{"everyone", "number", "name", "text", "type", "group",
		"wp_number", "wp_name", "send_seen", "add_aitag", "do_debug"} {
		if !strings.Contains(raw, field) {
			t.Errorf("shipped config missing %s field mention", field)
		}
	}

	// type: opencode'in tamamı dokuz allow_* bayrağı. Eksik bir bayrak,
	// kullanıcının "bu hat neden çalışmıyor" diye araması demektir.
	for _, bayrak := range []string{
		"allow_help", "allow_cmd", "allow_durum", "allow_camera",
		"allow_gorsel", "allow_ses", "allow_belge", "allow_reaction",
	} {
		if !strings.Contains(raw, bayrak+":") {
			t.Errorf("shipped config missing %s", bayrak)
		}
	}

	// Betik yolları ve allow_*'ın hangi betiği kullandığı açıklanmalı:
	// argümanlar config'de yazılmadığı için bu eşleşme belgeden anlaşılmalı.
	for _, ipucu := range []string{"$TRIGGER_ID", "$TRIGGER_TEXT", "$TRIGGER_JID"} {
		if !strings.Contains(raw, ipucu) {
			t.Errorf("shipped config missing argument note %s", ipucu)
		}
	}
}

func TestShippedConfigCoversTheLIDTrap(t *testing.T) {
	raw := readShippedConfig(t)

	if !strings.Contains(raw, "@lid") {
		t.Error("real LID example must be in shipped config")
	}
	if !strings.Contains(strings.ToUpper(raw), "DERIVABLE") && !strings.Contains(strings.ToUpper(raw), "TÜRET") {
		t.Error("LID cannot be derived warning must be documented")
	}
	if !strings.Contains(strings.ToUpper(raw), "PRIORITY") && !strings.Contains(strings.ToUpper(raw), "ÖNCELİK") {
		t.Error("number->name priority must be documented")
	}
}

func TestShippedConfigDocumentsTheEnvironmentVariables(t *testing.T) {
	raw := readShippedDocs(t)

	for _, key := range []string{"TRIGGER_TEXT", "TRIGGER_MESSAGES", "TRIGGER_JSON", "mesajlar", "stdin"} {
		if !strings.Contains(raw, key) {
			t.Errorf("shipped config must document %s", key)
		}
	}
}

// yamlOrnegiCikar, docs'taki ```yaml ... ``` bloklarından aranan ifadeyi
// içeren bloğu verir. Amaç: dokümandaki örnek GERÇEKTEN yüklenebilsin —
// örnek yazıldıktan sonra config değişirse sessizce yanlış kalmasın.
func yamlOrnegiCikar(t *testing.T, docs, aranan string) string {
	t.Helper()

	bas := strings.Index(docs, "```yaml")
	for bas >= 0 {
		s := strings.Index(docs[bas:], "\n")
		ic := bas + s + 1
		son := strings.Index(docs[ic:], "```")
		if son < 0 {
			break
		}
		blok := docs[ic : ic+son]
		if strings.Contains(blok, aranan) {
			return blok
		}
		bas = strings.Index(docs[ic+son:], "```yaml")
		if bas >= 0 {
			bas += ic + son
		}
	}
	t.Fatalf("config.readme.md içinde %q içeren bir ```yaml bloğu yok", aranan)
	return ""
}

// TestDokumandakiOpencodeOrnegiYukleniyor, dokümandaki tek kural örneğinin
// GERÇEK config yükleyicisinden geçtiğini sınar.
//
// Neden: bu örnek, sahibinin göreceği tek tam örnektir. Örnek yazıldıktan
// sonra bir alan zorunlu hâle gelirse (2026-10-06'da `session` böyleydi)
// örnek geçersiz kalır ve okuyan config'i kopyalayıp "hata alıyorum"
// der. Doküman ile kod arasındaki tek güvenilir köprü budur.
func TestDokumandakiOpencodeOrnegiYukleniyor(t *testing.T) {
	blok := yamlOrnegiCikar(t, readShippedDocs(t), "type: opencode")

	// Örnek üstünde rules: anahtarı olmayabilir (kural listesi parçası olarak
	// yazılmış olabilir); yükleyici tam config ister.
	if !strings.Contains(blok, "rules:") {
		blok = "rules:\n" + blok
	}

	yol := filepath.Join(t.TempDir(), "ornek.yaml")
	if err := os.WriteFile(yol, []byte(blok), 0o600); err != nil {
		t.Fatalf("örnek yazılamadı: %v", err)
	}

	rs, err := loadConfig(yol)
	if err != nil {
		t.Fatalf("dokümandaki opencode örneği yüklenemedi: %v\n--- örnek ---\n%s", err, blok)
	}
	if len(rs.Rules) != 1 {
		t.Errorf("örnek 1 kural içermeli, %d içeriyor", len(rs.Rules))
	}
}

// TestDokumanZorunluAlanlariBelirtiyor, kodda zorunlu olan alanların
// dokümanda da ZORUNLU diye anlatıldığını sınar.
//
// 2026-10-06 ölçümü: `session` koddan zorunluydu ama örnekte yoktu ve
// dokümanda "zorunlu" kelimesi geçmiyordu. Okuyan örnekten kopyalayınca
// `session required` hatası alıyordu — hatanın nedenini dokümanda
// okuyamıyordu.
func TestDokumanZorunluAlanlariBelirtiyor(t *testing.T) {
	raw := readShippedDocs(t)

	if !strings.Contains(raw, "ZORUNLU") && !strings.Contains(raw, "zorunlu") {
		t.Error("doküman zorunlu alanları 'zorunlu' diye adlandırmıyor")
	}

	// session, koddan bakıldığında ilk doğrulanan zorunlu alandır.
	if !strings.Contains(raw, "session") {
		t.Error("doküman 'session' alanını hiç geçmiyor")
	}
}

// TestDokumanYeniAlanlariBelirtiyor, 2026-10-06'da eklenen alanların
// dokümanda görünür olduğunu sınar. Alan eklendi ama doküman unutulursa
// bu test kırmızıya döner.
func TestDokumanYeniAlanlariBelirtiyor(t *testing.T) {
	raw := readShippedDocs(t)

	for _, alan := range []string{
		"wp_number",      // match: number'ın okunur adı
		"wp_name",        // match: name'in okunur adı
		"send_seen",      // eylem bazlı *bool geçersiz kılma
		"add_aitag",      // ↑
		"do_debug",       // ↑
		"--format json",  // çıktı ayrıştırmanın dayanağı
		"allow_reaction", // yetkenin en az biri
	} {
		if !strings.Contains(raw, alan) {
			t.Errorf("config.readme.md %q alanını belirtmiyor", alan)
		}
	}
}
