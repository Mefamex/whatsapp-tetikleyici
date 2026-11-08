"""gorsel — WhatsApp görsel/belge/sesli video → Türkçe cevap.

Ses hattının (voice_pipeline.py) görsel karşılığı. Aynı sözleşme:
    tetikleyici → bu paket → stdout'a tek cevap

Tasarım kararı — SORU ZAMAN AŞIMI YOK:
    Her katman kendi hatasını yutar ve bir sonrakine düşer. Tümü
    başarısız olursa stdout'a tek satır Türkçe açıklama yazılır —
    asla boş çıktı. Boş çıktı, kullanıcının "cevap vermiyor" deyip
    nedenini bilemediği en pahalı hatadır (bkz. docs/KRITIK-SORUNLAR
    #6: 124 tetiklemenin 25'i sessizce düştü).

Alt modüller:
    cek.py      medyayı köprüden indir, türünü anla
    onizleme.py  boyut/format düzeltme, videodan kare çıkarma
    ocr.py      tesseract ile metin çıkarma
    gorme.py    ollama vision modeline sor
    istem.py    kullanıcının sorusunu bul, model istemini kur
    __main__.py argparse ve orkestrasyon
"""

__all__ = ["cek", "onizleme", "ocr", "gorme", "istem"]
