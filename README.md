# whatsapp-tetikleyici

|              |                                                               |
| ------------ | ------------------------------------------------------------- |
| **Author**   | [Mehmet Akif Akkoç (Mefamex)](https://github.com/Mefamex)     |
| **Website**  | [https://github.com/Mefamex/whatsapp-tetikleyici](https://github.com/Mefamex/whatsapp-tetikleyici) |
| **License**  | MIT                                                           |
| **Created**  | 2026-11-08                                                    |
| **Updated**  | 2026-11-08                                                    |
| **Stack**    | Go 1.22+, Python 3.10+, Whisper, Ollama, PyTorch, Bash        |
| **Platform** | Linux, macOS                                                  |
| **Status**   | Production Ready                                              |

> WhatsApp Bridge olay akışını (SSE) dinleyen; gelen mesajları kabuk komutlarına (`/cmd`), interaktif bash kabuğuna (`/cmdhub`), yapay zekâ oturum tünellerine (`/session`), yerel görsel analize (Ollama vision) ve ses transkripsiyonuna (Whisper) yönlendiren akıllı otomasyon ve olay dağıtım (dispatcher) motoru.

---

## 1. Mimari ve Görsel Önizleme (Architecture & Preview)

```text
┌─────────────────────────┐
│  WhatsApp Bridge (API)  │
│  (SSE: /events)         │
└────────────┬────────────┘
             │ Server-Sent Events
             ▼
┌────────────────────────────────────────────────────────┐
│             WhatsApp Tetikleyici Motoru (Go)           │
│  - Kural & Kimlik Filtresi (Whitelist / Regex)         │
│  - Catch-up Yaş Filtresi (Eski Mesaj Koruması)         │
│  - Tünel Durum Denetleyicisi (.tetikleyici/tunel)      │
└────────────┬──────────────────┬──────────────────┬─────┘
             │                  │                  │
      [ /cmd & /cmdhub ]    [ Fotoğraf ]       [ Sesli Not ]
             │                  │                  │
             ▼                  ▼                  ▼
      ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
      │   Bash CLI   │   │ Ollama V-LLM │   │ Whisper ASR  │
      │  ve Ajanlar  │   │  (Qwen2.5-VL)│   │ (VRAM Boşalt)│
      └──────┬───────┘   └──────┬───────┘   └──────┬───────┘
             │                  │                  │
             └──────────────────┼──────────────────┘
                                ▼
                   ┌─────────────────────────┐
                   │  Formatlanmış Yanıt     │
                   │  (POST /send -> WP)     │
                   └─────────────────────────┘
```

- **Sıfır Mesaj Kaybı:** SSE bağlantısı kopsa bile otomatik yeniden bağlanır ve bayat mesajları süzer.
- **Akıllı VRAM Tasarrufu:** Whisper ve görüntü modelleri GPU'da gereksiz yer tutmaz; iş bitince bellek derhal tahliye edilir.

---

## 2. Belge Haritası (Repository Doc Map)

| Belge | Amacı ve Sorumluluğu |
| :--- | :--- |
| [`README.md`](./README.md) | Proje vitrini, komut referansı, kurulum ve mimari kılavuz |
| [`TODO.md`](./TODO.md) | Aktif sprint, bağlam çapası ve gelecek özellikler |
| [`TODO.DONE.md`](./TODO.DONE.md) | Doğrulanmış iş arşivi (ters kronolojik, test çıktılarıyla) |
| [`ASK.md`](./ASK.md) | Açık mimari sorular ve alınan otonom teknik kararlar |
| [`NOTES.md`](./NOTES.md) | Dahili teknik bellek, tünel mekanizması ve GPU stratejisi |
| [`config.yaml`](./config.yaml) | Kaskad yapılandırma ayarları ve çevre değişkenleri |
| [`config.example.yaml`](./config.example.yaml) | Kapsamlı kural ve filtreleme şablonu (`tetikleyici.yaml`) |
| [`Makefile`](./Makefile) | Tek komutla bağımlılık, derleme, test ve temizlik yönetimi |

---

## 3. Temel Yetenekler ve Komut Merkezi

### Komut Referansı (WhatsApp Üzerinden)

| Komut | Parametre | Açıklama |
| :--- | :--- | :--- |
| `/cmd` | `<komut>` | Tek seferlik kabuk komutu çalıştırır (özet WhatsApp'a, tam çıktı diske) |
| `/cmdhub` | - | Doğrudan bash tüneli açar; yazılan her metin kabukta koşulur |
| `/cmdhub out` | - | Son komutun tam terminal çıktısını WhatsApp'a döker |
| `/cmdhub cd` | `<dizin>` | Tünelin çalışma dizinini değiştirir |
| `/session list` | `[n]` | Açık yapay zekâ ajan oturumlarını listeler |
| `/session go` | `<no \| id>` | Mesajları doğrudan seçilen ajanın oturumuna tüneller |
| `/session exit`| - | Aktif oturum tünelini kapatır, normal moda döner |
| `/exit` | - | Açık olan her türlü tünelden (`cmdhub` veya `session`) çıkar |
| `/agent` | `<secim>` | Ajan motorunu değiştirir (`opencode`, `agy`, `claude`) |
| `/ping` | - | Tetikleyicinin canlılığını ve yanıt süresini test eder |

---

## 4. Teknoloji Yığını ve Gereksinimler (Tech Stack & Prerequisites)

* **Go:** 1.22+
* **Python:** 3.10+ (ve `uv` paket yöneticisi)
* **Ollama (Opsiyonel):** Görsel analiz için (`qwen2.5-vl:7b`)
* **FFmpeg & PyTorch (Opsiyonel):** Sesli mesaj transkripsiyonu için
* **WhatsApp Bridge:** Çalışan bir `whatsapp-bridge-mcp` servisi (`http://127.0.0.1:31415`)

---

## 5. Kurulum ve Hızlı Başlangıç (Installation & Quick Start)

### 1. Depoyu Klonlayın
```bash
git clone https://github.com/Mefamex/whatsapp-tetikleyici.git
cd whatsapp-tetikleyici
```

### 2. Bağımlılıkları Kurun
```bash
make install
```

### 3. Yapılandırmayı Ayarlayın
`config.example.yaml` dosyasını `tetikleyici.yaml` olarak kopyalayın ve yetkili numaranızı ekleyin:

```bash
cp config.example.yaml tetikleyici.yaml
```

```yaml
# tetikleyici.yaml içinde:
- name: sahip
  match:
    wp_number: ["905551234567"]
  do:
    - type: opencode
      allow_cmd: true
      allow_gorsel: true
      allow_ses: true
```

### 4. Tetikleyiciyi Başlatın
```bash
make run
```

---

## 6. Yapılandırma ve Çevre Değişkenleri

| Parametre | Ortam Değişkeni | Varsayılan | Açıklama |
| :--- | :--- | :--- | :--- |
| `bridge.url` | `BRIDGE_URL` | `http://127.0.0.1:31415` | WhatsApp Bridge API adresi |
| `bridge.catchup` | `CATCHUP_MAX_AGE_MIN` | `10` | Eski mesajların atlanma süresi (dakika) |
| `pipelines.whisper` | `WHISPER_MODEL` | `base` | Whisper ses modeli boyutu |
| `pipelines.vision` | `OLLAMA_VISION_MODEL` | `qwen2.5-vl:7b` | Ollama görüntü modeli |
| `pipelines.keep_alive`| `GORSEL_KEEP_ALIVE` | `15m` | Ollama modelinin bellekte kalma süresi |

---

## 7. Kullanım ve Yaşam Döngüsü Komutları

```bash
make            # Kullanılabilir tüm komutları listeler
make build      # Go motorunu bin/tetikleyici olarak derler
make run        # Tetikleyiciyi arka planda başlatır
make test       # Go (27 test) ve Python (153 test) testlerini koşturur
make verify     # Commit öncesi lint ve test doğrulamalarını çalıştırır
make clean      # Geçici önbellekleri ve logları temizler
```

---

## 8. Güvenlik, Gizlilik ve Kırmızı Çizgiler (Safety & Privacy)

1. **Beyaz Liste Zorunluluğu:** Yetkisiz numaralardan gelen `/cmd` ve sistem komutları kesinlikle engellenir ve yanıt verilmez.
2. **Kişisel Veri (PII) Temizliği:** Depoda, yapılandırmada ve testlerde asla gerçek telefon numarası veya gizli kimlik bulunmaz.
3. **Çıktı İzolasyonu:** Kabuk çıktıları `whatsapp-cmd/` dizininde saklanır ve asla dış sunuculara iletilmez.

---

## 9. Lisans ve Katkı (License)

Bu proje [MIT Lisansı](./LICENSE) altında sunulmuştur.  
Geliştirici: **Mehmet Akif Akkoç (Mefamex)**
