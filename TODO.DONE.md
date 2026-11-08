# WhatsApp Tetikleyici — Doğrulanmış İş Arşivi (TODO.DONE.md)

> **Kural:** Ters kronolojik sıralama (en yeni en üstte). Her tamamlanan işin altında çalıştırılan doğrulama komutu ve test kanıtı yer alır.

---

### [2026-11-08 04:16] Proje İskeleti, PII Hijyeni ve Doğrulama Zinciri

- **Açıklama:** WhatsApp Tetikleyici Go olay motoru, Python komut merkezi (`cmd`, `tunel`, `session_manager`, `cmdhub`), görsel ve ses boru hatları (`voice_pipeline.py`, `gorsel_pipeline.py`) `whatsapp-tetikleyici` reposunda toplandı. Kod tabanındaki ve testlerdeki kişisel telefon/LID verileri standart test değerleriyle değiştirildi. Marmelab self-documenting Makefile yazıldı ve `make verify` ile tüm Go ve Python testleri (153 birim testi) eksiksiz doğrulandı.
- **Doğrulama Komutu:** `make verify`
- **Çıktı / Kanıt:**
  ```text
  ==> Go vet analizi yapılıyor...
  cd tetikleyici && go vet ./...
  ==> Python sözdizimi denetleniyor...
  .venv/bin/python -m py_compile *.py gorsel/*.py tetikleyici/cmd/*.py
  ==> Go birim testleri koşturuluyor...
  cd tetikleyici && go test ./...
  ok  	tetikleyici	3.994s
  ==> Python boru hattı ve tünel testleri koşturuluyor...
  PYTHONPATH=.:tetikleyici/cmd .venv/bin/python -m unittest discover -s tests -p 'test_*.py' -v
  Ran 153 tests in 0.514s - OK (skipped=2)
  ==> TÜM DOĞRULAMALAR BAŞARIYLA GEÇTİ (ALL_OK).
  ```
