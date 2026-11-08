# WhatsApp Tetikleyici — Dahili Teknik Bellek (NOTES.md)

> **Amaç:** Mimari kararlar, teknik detaylar, elenen alternatifler ve sistem kısıtlarının kalıcı teknik kaydıdır.

---

## 1. Mimari Tasarım ve Olay İşleme Modeli

- **Neden Bağımsız Go Dispatcher?**
  WhatsApp köprüsü ile doğrudan yapay zekâ oturumunu birbirine bağlamak kırılgandır. Go tabanlı `tetikleyici` motoru, köprünün Server-Sent Events (SSE) akışını dinler; gelen mesajları regex, beyaz liste (whitelist) ve kullanıcı profillerine göre süzer. Yalnızca yetkili mesajlar ilgili alt sistemlere yönlendirilir.

- **Tünel Durum Makinesi (`tunel.py` & `tunel.go`):**
  Kullanıcı `/cmdhub` yazdığında sohbet bir bash tüneline dönüşür; `/session go <id>` yazdığında doğrudan ilgili AI ajanının açık oturumuna bağlanır. Bu durum `.tetikleyici/tunel_durumu.json` dosyasında atomik olarak tutulur. Go motoru gelen mesajı işlemeden önce bu durumu okur ve normal mesajları bile doğrudan açık tünele aktarır.

- **Catch-up Yaş Filtresi (`catchUpMaxAge`):**
  Tetikleyici yeniden başlatıldığında veya ağ kopup geri geldiğinde Whatsmeow eski okunmamış mesajları SSE akışında topluca basabilir. Tetikleyici, başlangıç anından itibaren 10 dakikadan eski mesajları tespit eder; bunları `seenStore`'a işleyip sessizce atlayarak açılışta eski mesaj spam'i oluşmasını engeller.

---

## 2. GPU ve Bellek Yönetimi

- **Whisper VRAM Boşaltımı (`voice_pipeline.py`):**
  Sesli mesaj geldiğinde Whisper modeli belleğe alınır, transkripsiyon biter bitmez `del model` ve `torch.cuda.empty_cache()` çağrılarak VRAM sisteme geri iade edilir.
- **Ollama Timeout (`gorme.py`):**
  Görsel analizlerde `GORSEL_KEEP_ALIVE` varsayılan olarak `15m` tutulur; böylece peş peşe gelen fotoğraflarda model her seferinde baştan yüklenmez, iş bitince GPU boşalır.
