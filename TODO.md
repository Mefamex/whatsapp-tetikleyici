# WhatsApp Tetikleyici — Yapılacaklar (TODO.md)

<!--
  BAĞLAM ÇAPASI (CONTEXT ANCHOR):
  Nihai Amaç: WhatsApp Bridge SSE olay akışını dinleyerek mesajları yakalayan, yetkili komutları (/cmd, /session, /cmdhub, /agent), görsel analizleri (Ollama vision) ve ses transkripsiyonunu (Whisper) yürüten; yanıtları formatlayıp WhatsApp'a geri basan bağımsız olay motoru.
  Mevcut Durum: Go çekirdek motoru ve Python boru hatları 'whatsapp-tetikleyici' reposunda birleştirildi; PII verilerinden arındırıldı, tüm testler (153 Python + 27 Go testi) başarıyla doğrulandı.
  Kırmızı Çizgiler:
    - Kişisel telefon numaraları, LID JID'leri veya API tokenları koda gömülemez.
    - .tetikleyici/ durum dosyaları ve whatsapp-cmd/ çıktıları depoya eklenemez.
    - 'make verify' (lint + test) geçmeden commit/push yapılamaz.
-->

---

## 1. Aktif Sprint (Current Sprint)

- [ ] **Daemonize / Systemd Şablonu:** Tetikleyicinin `systemd --user` servisi olarak tek komutla kurulmasını sağlayan `systemd/whatsapp-tetikleyici.service` dosyasını ekleme.
- [ ] **Gelişmiş Çoklu Ajan Tüneli:** OpenCode'un yanı sıra Antigravity ve Claude CLI oturumları arasında tek komutla geçiş (`/session switch <cli>`).
- [ ] **Telegram Yedek Bildirim Köprüsü:** WhatsApp köprüsü kapandığında hata durumunu Telegram botu üzerinden bildiren opsiyonel bildirim kanalı.

---

## 2. Gelecek Planları (Backlog)

- [ ] **WebUI / Terminal Dashboard:** Aktif tünelleri, son tetiklemeleri ve GPU bellek kullanımını gösteren minimal TUI/Web arayüzü.
- [ ] **Eklenti Sistemi:** Kullanıcıların kendi Python veya Go komutlarını tek bir `.py` dosyası atarak tetikleyiciye ekleyebileceği `plugins/` dizini.
