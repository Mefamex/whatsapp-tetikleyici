# WhatsApp Tetikleyici — Soru ve Karar Kuyruğu (ASK.md)

> **Kural:** "Tahminle koda girilmez." Çözüm bekleyen mimari belirsizlikler ve alınan otonom kararlar bu dosyada kayıt altına alınır.

---

## 1. Açık Sorular (Cevap Bekleyenler)

### [2026-11-08] agy geçmişindeki büyük WhatsApp sohbetleri silinsin mi?
`~/.gemini/antigravity/conversation_summaries.db` ve `agyhub_summaries_proto.pb` içinden `WhatsApp'tan…` ile başlayan **11 küçük (2 adımlık)** sohbet silindi. Yedek: `~/.gemini/antigravity/scratch/agy-gecmis-yedek-20261108/`.
Aşağıdaki **6 sohbet** WhatsApp'tan başlamış ama arkasında gerçek iş yapılmış; kullanıcı kararı bekleniyor:
- `8bf93805` (10 adım) · `338def07` (18) · `d32732af` (38) · `dd1f552e` (40) · `eebd8bde` (48) · `f51bc831` (132)
- **Soru:** Hepsi silinsin mi, bir kısmı mı, hiçbiri mi?
- **Not:** Sorun kalıcı çözülmedikçe (TODO #7, sabit oturum) her yeni WhatsApp mesajı geçmişi tekrar kirletir.

---

## 2. Kendi Kararım (Otonom Alınan Kararlar)

### [2026-11-08] Dizin Yapısı ve Modül Hiyerarşisi
- **Seçenek A:** Boru hatlarını `pipelines/` alt dizinine taşımak.
- **Seçenek B:** Boru hatlarını kök dizinde (`voice_pipeline.py`, `gorsel_pipeline.py`, `gorsel/`) tutmak.
- **Karar:** Seçenek B (Kök dizin yapısı).
- **Gerekçe:** 150+ birim testinde ve Go konfigürasyon eşlemelerinde dosya yolları kök üzerinden kurgulanmıştır. Kök dizin yapısı korunarak hem geriye dönük uyumluluk sağlanmış hem de test karmaşıklığı sıfırlanmıştır.
