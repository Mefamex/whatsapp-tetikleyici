# WhatsApp Tetikleyici — Soru ve Karar Kuyruğu (ASK.md)

> **Kural:** "Tahminle koda girilmez." Çözüm bekleyen mimari belirsizlikler ve alınan otonom kararlar bu dosyada kayıt altına alınır.

---

## 1. Açık Sorular (Cevap Bekleyenler)

*Şu an açık bir mimari soru bulunmamaktadır.*

---

## 2. Kendi Kararım (Otonom Alınan Kararlar)

### [2026-11-08] Dizin Yapısı ve Modül Hiyerarşisi
- **Seçenek A:** Boru hatlarını `pipelines/` alt dizinine taşımak.
- **Seçenek B:** Boru hatlarını kök dizinde (`voice_pipeline.py`, `gorsel_pipeline.py`, `gorsel/`) tutmak.
- **Karar:** Seçenek B (Kök dizin yapısı).
- **Gerekçe:** 150+ birim testinde ve Go konfigürasyon eşlemelerinde dosya yolları kök üzerinden kurgulanmıştır. Kök dizin yapısı korunarak hem geriye dönük uyumluluk sağlanmış hem de test karmaşıklığı sıfırlanmıştır.
