# whatsapp-tetikleyici

|              |                                                               |
| ------------ | ------------------------------------------------------------- |
| **Author**   | [Mehmet Akif Akkoç (Mefamex)](https://github.com/Mefamex)     |
| **Website**  | [https://github.com/Mefamex/whatsapp-tetikleyici](https://github.com/Mefamex/whatsapp-tetikleyici) |
| **License**  | MIT                                                           |
| **Created**  | 2026-11-08                                                    |
| **Updated**  | 2026-11-08                                                    |
| **Stack**    | Go 1.22+, Python 3.10+, Systemd, Whisper, Ollama, PyTorch, Bash |
| **Platform** | Linux, macOS                                                  |
| **Status**   | Production Ready                                              |

> **İnternete bağlı kişisel bilgisayarınız ile WhatsApp arasında çalışan, ister bağımsız yapay zekâ ajanlarıyla ister yapay zekâya ihtiyaç duymadan doğrudan sistem seviyesinde çalışan çift yönlü köprü, otomasyon ve olay dağıtım (dispatcher) platformu.**

---

## 1. Temel Amaç ve Felsefe (Core Vision)

Bu proje, WhatsApp'ı kişisel bilgisayarınız için güvenli, hızlı ve esnek bir uzaktan kumandaya dönüştürmek üzere geliştirilmiştir:

1. **WhatsApp ↔ Bilgisayar Köprüsü:** Kişisel bilgisayarınızı dışarıdan erişilebilir bir sunucu gibi kullanmanızı sağlar. Karmaşık VPN, SSH veya port yönlendirme ayarları yapmadan doğrudan WhatsApp sohbetiniz üzerinden bilgisayarınızla haberleşirsiniz.
2. **Kişiselleştirilmiş Tetikleme & Sabit Oturumlar (Persistent Sessions):** Gelen mesajın kimden geldiğine (numara / WhatsApp LID) göre özelleştirilmiş kurallar çalışır. Tanımlı her kişi için sabit bir oturum kimliği (`session ID`) tutulabilir; böylece konuşmalar her mesajda sıfırlanmaz, geçmiş bağlam korunur.
3. **AI Olmadan da Doğrudan Bilgisayar Yönetimi (Admin Komutları):** Bilgisayar açıldığında tetikleyici `systemd` servisiyle arka planda otomatik başlar. Yapay zekâ modellerine ihtiyaç kalmadan, admin yetkisine sahip numara tek mesajla doğrudan terminal komutları (`/cmd`), web kamerası fotoğrafı (`/camera`), sistem sağlık durumu (`/durum`) çalıştırabilir.
4. **Dinamik Model ve Ajan Değişimi:** Sohbet içindeyken tek bir komutla aktif çalışan yapay zekâ motoru (`/agent agy`, `/agent claude`, `/agent opencode`), oturum tüneli (`/session go`) ve dil modeli anında değiştirilebilir.
5. **Çoklu Ortam (Multimodal) Hatları:** WhatsApp sesli notları yerel Whisper ile anında metne dökülür; fotoğraflar ve belgeler yerel görüntü modelleriyle (Ollama / Qwen2.5-VL) analiz edilir.
6. **Sıfır Kişisel Veri Sızıntısı (Zero-Leak Privacy):** Gerçek telefon ve LID numaraları repoda tutulmaz; `.gitignore` korumalı yerel yapılandırma ve örnek şablon (`tetikleyici.example.yaml`) ile tam izolasyon sağlanır.

---

## 2. Mimari ve Olay Akışı (Architecture & Workflow)

### Genel Ekosistem Mimarisi (4 Katmanlı Hibrit Model)

```text
                               ┌──────────────────────────────────────────────┐
                               │            WHATSAPP (Mobil / Web)            │
                               └──────────────────────┬───────────────────────┘
                                                      │ WebSocket
                                                      ▼
                       ┌──────────────────────────────────────────────────────────────┐
                       │        KATMAN 1: whatsapp-bridge (Whatsmeow / Go)            │
                       │     - REST API & Gerçek Zamanlı SSE Dinleyici (:31415)       │
                       └──────────────┬───────────────────────────────┬───────────────┘
                                      │                               │
        [İki Yönlü MCP Araçları]      │                               │ [Gelen Olay Akışı (SSE)]
                                      ▼                               ▼
       ┌────────────────────────────────────────────┐  ┌─────────────────────────────────────────────┐
       │     KATMAN 2: whatsapp-mcp (Agent-to-WA)   │  │  KATMAN 3: whatsapp-tetikleyici (Dispatcher)│
       │  - FastMCP STDIO Sunucusu                  │  │  - Whitelist & RBAC (Telefon & LID)         │
       │  - Ajanların WhatsApp'ı Kullanması         │  │  - VRAM Tahliyeli Multimodal (Whisper/Qwen) │
       │  - İnsan Onayı (whatsapp_ask_confirm)      │  └──────────────┬──────────────────────────────┘
       └────────────────────────────────────────────┘                 │
                                                      ┌───────────────┴───────────────┐
                                                      ▼                               ▼
                                      ┌──────────────────────────────┐ ┌─────────────────────────────┐
                                      │ KATMAN 4A: whatsapp-to-cmd   │ │ KATMAN 4B: whatsapp-to-agent│
                                      │   (Doğrudan Sistem Kumandası)│ │     (Ajan & Oturum Tüneli)  │
                                      │ - /cmd & /cmdhub (Bash Tüneli│ │ - /agent agy|claude|opencode│
                                      │ - /camera, /ekran, /durum    │ │ - /session go & /reply      │
                                      └──────────────────────────────┘ └─────────────────────────────┘
```

### Olay Dağıtım ve Tetikleyici Akış Şeması (Dispatcher Detail)

```text
┌───────────────────────────────┐
│     WhatsApp Bridge (API)     │
│   (SSE Dinleyici: /events)    │
└───────────────┬───────────────┘
                │ Gerçek Zamanlı Olay Akışı (SSE)
                ▼
┌────────────────────────────────────────────────────────┐
│             WhatsApp Tetikleyici Motoru (Go)           │
│  - Beyaz Liste & RBAC Yetki Kontrolü (users)           │
│  - Catch-up Yaş Filtresi (Eski/Bayat Mesaj Önleme)     │
│  - Sabit Oturum & Tünel Durum Denetleyicisi            │
└───────┬───────────────┬───────────────┬────────────────┘
        │               │               │
  [ /cmd & /bash ] [ Fotoğraf/Belge ] [ Sesli Not ]
        │               │               │
        ▼               ▼               ▼
 ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
 │ Doğrudan CLI │ │ Ollama V-LLM │ │ Whisper ASR  │
 │ ve AI Motoru │ │ (Qwen2.5-VL) │ │(VRAM Tahliye)│
 │(AGY/Claude/OC)└──────┬───────┘ └──────┬───────┘
 └──────┬───────┘       │                │
        │               └───────┬────────┘
        ▼                       ▼
 ┌────────────────────────────────────────┐
 │           Formatlanmış Yanıt           │
 │       (POST /send -> WhatsApp)         │
 └────────────────────────────────────────┘
```

* **Sıfır Mesaj Kaybı:** Bağlantı kopsa dahi SSE otomatik yeniden bağlanır ve tamponlanan mesajları işler.
* **Akıllı Bellek Tahliyesi:** Whisper ve yerel görüntü modelleri işi bittiğinde GPU VRAM'ini serbest bırakır.

### Yükseltmeden Sonra Hedef Mimari (Headless Daemon + Birleşik CLI + Gömülü Web Dashboard)

> **Kritik Yükseltme Hedefi:** Ağır masaüstü pencereleri (Electron/Qt) yerine sistem yükünü ~30 MB RAM seviyesinde tutan, hem headless sunucularda (VPS, Raspberry Pi) hem de yerel masaüstünde sıfır ek yükle çalışan; WhatsApp ("ChatOps"), Birleşik CLI ve yerel Gömülü Web Dashboard'u birleştiren hibrit mimari.

```text
                               ┌──────────────────────────────────────────────────────────────┐
                               │               Kullanıcı & Yönetim Arayüzleri                 │
                               │  ┌──────────────────────┐    ┌─────────────────────────────┐ │
                               │  │ WhatsApp ("ChatOps") │    │ Yerel Gömülü Web Dashboard  │ │
                               │  │ /cmd, /camera, /agent│    │ (http://127.0.0.1:31415)    │ │
                               │  │ Anket, Emoji Tepkiler│    │ [Büyük QR Kod, Metrik, Log] │ │
                               │  └──────────┬───────────┘    └──────────────┬──────────────┘ │
                               │             │                               │                │
                               │             │                ┌──────────────┴──────────────┐ │
                               │             │                │    Birleşik Yönetim CLI     │ │
                               │             │                │   `tetikleyici status/qr`   │ │
                               │             │                └──────────────┬──────────────┘ │
                               └─────────────┼───────────────────────────────┼────────────────┘
                                             │ WhatsApp Web                  │ Yerel HTTP /
                                             │ Soketi (Multi-Device)         │ REST & SSE
                                             ▼                               ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│ 1. KÖPRÜ: whatsapp-bridge-mcp (Go Headless Daemon :31415)                                   │
│  - Whatsmeow Çekirdeği (SQLite WAL whatsmeow.db, ~30 MB RAM)                                │
│  - Gömülü Web Sunucusu (go:embed Web Dashboard & QR Motoru)                                 │
│  - HTTP REST API & Gerçek Zamanlı SSE Yayıncısı (/events)                                   │
│  - FastMCP STDIO Katmanı (AI Ajanları için whatsapp_* Araç Seti)                            │
└─────────────────────────────────────────────┬───────────────────────────────────────────────┘
                                              │ Gerçek Zamanlı Olay Akışı (SSE)
                                              ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────┐
│ 2. DAĞITICI: whatsapp-tetikleyici (Go Headless Daemon)                                      │
│  - Beyaz Liste & RBAC Yetki Denetimi (Telefon & LID Doğrulama)                              │
│  - Catch-up Filtresi (Eski/Bayat Mesajların Açılışta Önlenmesi)                             │
│  - Tünel & Durum Makinesi (/cmdhub Bash Tüneli & /session)                                  │
│  - Ajan Adaptör Mimarisi (Ortak AgentDriver Arayüzü & Sabit Session ID)                     │
└───────┬─────────────────────────────┬───────────────────────────────┬───────────────────────┘
        │                             │                               │
        ▼ [Sistem Komutları]          ▼ [Multimodal Hatlar]           ▼ [AI Ajan Sürücüleri]
 ┌──────────────┐              ┌─────────────────────────────┐ ┌─────────────────────────────┐
 │ Doğrudan CLI │              │   Yerel Çıkarım Boruları    │ │ Sabit Oturumlu AI Adaptörler│
 │ /cmd, /camera│              │   - Whisper (Ses -> Metin)  │ │ - OpenCode Driver           │
 │ /ekran, /ping│              │   - Ollama V-LLM (Görsel)   │ │ - AGY Driver (Antigravity)  │
 │ /zamanla     │              │   (İş bitince VRAM tahliye) │ │ - Claude Driver (Claude CLI)│
 └──────┬───────┘              └──────────────┬──────────────┘ └──────────────┬──────────────┘
        │                                     │                               │
        └─────────────────────────────────────┼───────────────────────────────┘
                                              │
                                              ▼
 ┌────────────────────────────────────────────────────────────────────────────────────────────┐
 │ Formatlanmış Yanıt & Geri Bildirim                                                         │
 │ (POST /send, POST /reaction [⏳->✅], POST /poll-vote -> WhatsApp Köprüsü)                │
 └────────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Masaüstü GUI Bağımlılığı Yok:** X11/Wayland gerektirmez, doğrudan Linux VPS veya Raspberry Pi üzerinde çalışabilir.
- **Sıfır Ekstra Bellek:** Gömülü Web UI doğrudan Go binary içine gömülüdür (`go:embed`), Node.js/Electron gibi fazladan RAM harcamaz.
- **Tek Noktadan Yönetim:** Tüm sistem `tetikleyici status`, `tetikleyici qr`, `tetikleyici logs` gibi birleşik CLI komutlarıyla terminalden kolayca idare edilir.

---

## 3. Komut Referansı (WhatsApp Üzerinden)

Yetkili admin numarası, WhatsApp üzerinden şu komutları doğrudan çalıştırabilir:

| Komut | Parametre | Açıklama |
| :--- | :--- | :--- |
| `/cmd` | `<komut>` | Tek seferlik kabuk komutu çalıştırır (özet WhatsApp'a, tam çıktı diske) |
| `/cmdhub` | - | İnteraktif bash kabuk tüneli açar; yazılan her metin kabukta koşulur |
| `/cmdhub cd` | `<dizin>` | Bash tünelinin çalışma dizinini değiştirir |
| `/cmdhub out`| - | Son çalıştırılan komutun tam terminal çıktısını sohbete döker |
| `/camera` | - | Bilgisayarın web kamerasından fotoğraf çeker ve anında WhatsApp'a gönderir |
| `/ekran` | `[saniye]` | Bilgisayarın masaüstü ekran görüntüsünü çeker ve WhatsApp'a fotoğraf olarak atar |
| `/durum` | - | Köprü ve tetikleyici servislerinin anlık çalışma durumunu raporlar |
| `/ping` | - | Tetikleyicinin canlılığını ve ağ gecikmesini anında ölçer |
| `/agent` | `[agy\|claude\|opencode\|durum]` | Aktif yapay zekâ ajan motorunu canlıda değiştirir veya durumu gösterir |
| `/session list` | `[n]` | Açık yapay zekâ oturumlarını listeler |
| `/session go` | `<no \| id>` | Mesajları seçilen ajanın oturumuna doğrudan tüneller |
| `/session exit`| - | Aktif oturum tünelinden çıkıp varsayılan ana hatta döner |
| `/zamanla` | `<süre> <mesaj>` | Belirtilen süre sonra WhatsApp'tan mesaj göndermeyi zamanlar (`/zamanla 15dk su iç`) |
| `/zamanla list`| - | Bekleyen zamanlanmış görevleri listeler |
| `/zamanla iptal`| `<id>` | Zamanlanmış bir görevi iptal eder |
| `/reply` | `[no] <cevap>` | Ajanın bekleyen bir sorusuna doğrudan cevap iletir |
| `/exit` | - | Açık olan her türlü tünelden (`cmdhub` veya `session`) çıkar |
| `/help` | - | Kullanılabilir komutların yardım metnini listeler |

---

## 4. Belge Haritası (Repository Doc Map)

| Belge | Amacı ve Sorumluluğu |
| :--- | :--- |
| [`README.md`](./README.md) | Proje vitrini, felsefesi, komut referansı, kurulum ve mimari kılavuz |
| [`TODO.md`](./TODO.md) | Aktif sprint, adaptör mimarisi planı ve gelecek özellikler |
| [`TODO.DONE.md`](./TODO.DONE.md) | Doğrulanmış iş arşivi (ters kronolojik, test çıktılarıyla) |
| [`ASK.md`](./ASK.md) | Açık mimari sorular ve alınan otonom teknik kararlar |
| [`NOTES.md`](./NOTES.md) | Dahili teknik bellek, tünel mekanizması ve GPU stratejisi |
| [`tetikleyici.example.yaml`](./tetikleyici.example.yaml) | Kapsamlı kural, kullanıcı ve filtreleme şablonu |
| [`Makefile`](./Makefile) | Tek komutla bağımlılık, derleme, test ve temizlik yönetimi |

---

## 5. Teknoloji Yığını ve Gereksinimler

* **Go:** 1.22+ (Çekirdek SSE ve olay dağıtım motoru)
* **Python:** 3.10+ (Boru hatları ve `uv` sanal ortamı)
* **WhatsApp Bridge:** `whatsapp-bridge-mcp` servisi (`http://127.0.0.1:31415`)
* **Systemd:** Otomatik arka plan servis yönetimi (Linux)
* **Ollama & Qwen2.5-VL (Opsiyonel):** Yerel görsel analiz boru hattı
* **FFmpeg & Whisper (Opsiyonel):** Sesli not transkripsiyon boru hattı

---

## 6. Kurulum ve Hızlı Başlangıç

### 1. Depoyu Klonlayın
```bash
git clone https://github.com/Mefamex/whatsapp-tetikleyici.git
cd whatsapp-tetikleyici
```

### 2. Kurulumu Yapın
`make install` komutu Go bağımlılıklarını çözer, Python `.venv` ortamını kurar ve eğer mevcut değilse `tetikleyici.example.yaml` dosyasını otomatik olarak `tetikleyici.yaml` olarak kopyalar:

```bash
make install
```

### 3. Yapılandırmayı Düzenleyin
Oluşturulan `tetikleyici.yaml` dosyasında yetkili telefon ve LID numaranızı girin (`tetikleyici.yaml` dosyası `.gitignore` ile korumalıdır; repoya sızmaz):

```yaml
users:
  users:
    - number: ["905551234567", "12345678901234@lid"]
      name: ["Admin"]
      allow_slash: true
      allow_cmd: true
      allow_camera: true
      allow_durum: true
      allow_zamanla: true

rules:
  - name: admin-kurali
    match:
      wp_number: ["905551234567", "12345678901234@lid"]
    do:
      - type: opencode
        allow_cmd: true
        allow_gorsel: true
        allow_ses: true
```

### 4. Servisi Başlatın ve Otomatik Başlatmayı Açın
Tetikleyiciyi arka planda kullanıcı servisi olarak ayarlamak için:

```bash
# Elle anlık başlatmak için:
make run

# Bilgisayar açıldığında otomatik başlaması için (Systemd User Service):
systemctl --user enable --now whatsapp-tetikleyici.service
```

---

## 7. Yaşam Döngüsü ve Yönetim Komutları

```bash
make            # Kullanılabilir tüm Makefile komutlarını listeler
make build      # Go motorunu bin/tetikleyici olarak derler
make run        # Tetikleyici daemon'ını başlatır
make test       # Go ve Python birim testlerini çalıştırır
make verify     # Commit öncesi lint ve test doğrulamalarını koşturur
make clean      # Geçici önbellekleri, test artıklarını ve logları temizler
```

---

## 8. Güvenlik, Gizlilik ve Kırmızı Çizgiler (Safety & Privacy)

1. **RBAC ve Beyaz Liste Zorunluluğu:** Bilinmeyen veya yetkisi olmayan numaralardan gelen `/cmd`, `/camera` veya tünel komutları tamamen yok sayılır; yanıt verilmez.
2. **PII İzolasyonu:** Depoda, yapılandırmada ve testlerde asla gerçek telefon numarası veya kişisel WhatsApp LID kimlikleri bulunmaz.
3. **Lokal İzolasyon:** Kabuk çıktıları ve geçici medya dosyaları yerel diskte tutulur, hiçbir 3. parti buluta gönderilmez.

---

## 9. Gelecek Yol Haritası ve Gelişmiş Özellikler (Roadmap)

- [ ] **🛡️ İnteraktif Onay Sistemi:** Riskli eylemlerde WhatsApp'a 4 seçenekli anket atarak onay isteme (Tek seferlik onayla, Reddet, 1 saatliğine tümünü onayla, 1 haftalığına tümünü onayla).
- [ ] **📸 `/ekran` Masaüstü Yakalama:** Monitörün anlık ekran görüntüsünü WhatsApp'a yollama (`spectacle`/`grim`).
- [ ] **⏱️ Yapay Zekâ Destekli Cron:** Periyodik otonom görevler (`/zamanla her sabah 08:30 günün özetini hazırla`).
- [ ] **⏳ Canlı İlerleme ve Hızlı Tepki Emojisi:** Mesaj işlenirken otomatik `👀` / `⏳`, tamamlandığında `✅` reaksiyonu koyma ve uzun süren işlerde canlı durum güncellemesi (`tetikleyici.yaml` içinden `tepki_emojisi` ile tamamen özelleştirilebilir).

---

## 10. Lisans

Bu proje [MIT Lisansı](./LICENSE) altında sunulmuştur.  
Geliştirici: **[Mehmet Akif Akkoç (Mefamex)](https://github.com/Mefamex)**
