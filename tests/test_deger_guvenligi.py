#!/usr/bin/env python3
"""test_deger_guvenligi.py — deger_guvenligi kuralının regresyon testleri.

Buradaki cevaplar ÖLÇÜLMÜŞTÜR: tools/prompt_varyant.py'nin d-kucuk.png
koşularından ve tools/ocr_yol_deneyi.py'den alıntıdır. Elle uydurulmamıştır.
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from gorsel import deger_guvenligi as dg  # noqa: E402

# d-kucuk.png'den tesseract'ın okuduğu metin (ölçümde 5/5 doğruydu)
OCR_KUCUK = """KULLANICI ADI SOYADI
MARUZ ATILMA KODU: KX-7741
SERI NO: 88213-D
ODENEN: 4.960,50 TL
VADE TARIHI: 12.11.2026
IBAN: TR33 0006 1005 1978 6457 8413 26
AGIKLAMA: 12 AYLIK TAKSIT
"""

# 9/9 ölçülen başarısızlıklar
BASARISIZ = [
    "Görseldeki metin, kullanıcı adı ve soyadı, MARUZ ATILMA KODU, SERİ NO, "
    "ODENEN tutar, VADE TARIHI, IBAN, AÇIKLAMA, SUBE KODU ve MURETTID "
    "bilgilerini içeriyor.",
    "Görseldeki metin, kullanıcı adı ve soyadı, MARUZ ATILMA KODU, SERİ NO, "
    "ODENEN tutar, VADE TARIHI, IBAN, AÇIKLAMA, SUBE KODU ve MURETTID "
    "bilgilerini içeriyor. Metindeki 'Islencez Tesekkur Ederiz' ifadesi, "
    "gönderenin bir teşekkür mesajı olduğunu gösteriyor.",
    "Görseldeki metinde kullanıcı adı ve soyadı, MARUZ ATILMA KODU, SERİ NO, "
    "ODENEN tutar, VADE TARIHI, IBAN, AÇIKLAMA, SUBE KODU ve MURETTID "
    "bilgileri bulunmaktadır.",
    "Belgede, senin adın, maruz atılma kodu, seri numarası, ödendiğin tutar, "
    "vade tarihi, IBAN numaran ve taksit bilgisi yer alıyor.",
]

# Ölçülen başarılı cevaplar
BASARILI = [
    "Toplam ödenecek miktar 4.960,50 TL'dir.",
    "Toplam 4.960,50 TL.",
    "Ödeme miktarı 4.960,50 TL, ödeme tarihi 12.11.2026, IBAN numarası ve "
    "diğer ödeme detayları bulunuyor.",
]


class TestDegerCikarimi(unittest.TestCase):
    def test_degerleri_bulur(self):
        degerler = dg.degerleri(OCR_KUCUK)
        birlestir = " ".join(degerler)
        for beklenen in ("KX-7741", "4.960,50", "12.11.2026"):
            self.assertIn(beklenen, birlestir, f"{beklenen} bulunamadı: {degerler}")

    def test_alan_adi_deger_sayilmaz(self):
        # "ODENEN tutar" bir etiket; içindeki rakam yok zaten, ama
        # "VADE TARIHI: 12.11.2026" içindeki tarih DEĞERDİR.
        degerler = dg.degerleri(OCR_KUCUK)
        self.assertIn("12.11.2026", " ".join(degerler))

    def test_bos_metinde_bos_liste(self):
        self.assertEqual(dg.degerleri(""), [])
        self.assertEqual(dg.degerleri(None), [])


class TestKayipTespiti(unittest.TestCase):
    def test_olculmus_basarisiz_cevaplar_kayip(self):
        """9/9 ölçülen başarısızlıkların TAMAMI yakalanmalı.

        Bu testin varlık sebebi: emniyet ağı sessizce işe yaramayabilir.
        Kural yazdım diye işlediğini ölçülmüş örnekle kanıtlamadan
        üretime koymuyorum.
        """
        for i, cevap in enumerate(BASARISIZ, 1):
            with self.subTest(cevap=i):
                kayip, gerekce = dg.kayip_mi(cevap, OCR_KUCUK)
                self.assertTrue(kayip, f"kaçırıldı: {cevap[:70]}")
                self.assertTrue(gerekce, "gerekçe boş — loga yazılmalı")

    def test_olculmus_basarisiz_cevaplarda_hicbir_rakam_yok(self):
        # Kural 1'in gerekçesi: bu cevaplarda gerçekten rakam yok.
        # Kural 2 (değer eşleşmesi) bunları zaten yakalar.
        for cevap in BASARISIZ:
            with self.subTest(cevap=cevap[:50]):
                self.assertEqual(dg._rakamlar(cevap), "", "bu cevapta rakam vardı")

    def test_olculmus_basarili_cevaplar_kayip_degil(self):
        for cevap in BASARILI:
            with self.subTest(cevap=cevap[:50]):
                kayip, gerekce = dg.kayip_mi(cevap, OCR_KUCUK)
                self.assertFalse(kayip, f"yanlış tetiklendi ({gerekce}): {cevap[:60]}")

    def test_bos_cevap_kural_disinda(self):
        # Boş cevap zaten pipeline'ın başka katmanında yakalanıyor.
        kayip, _ = dg.kayip_mi("", OCR_KUCUK)
        self.assertFalse(kayip)

    def test_ocr_metni_yoksa_kural_disinda(self):
        # OCR yoksa bu kuralın dayanağı yok.
        kayip, _ = dg.kayip_mi("Bir kırmızı daire ve mavi kare var.", "")
        self.assertFalse(kayip)

    def test_tek_degerli_ocr_kural_disinda(self):
        # Yalnız bir değer varsa "değer taşınmadı" demek manasız —
        # cevap o değeri kelime olarak söylemiş olabilir.
        kayip, _ = dg.kayip_mi("Bu bir teknik servis formu.", "Toplam: 5 TL")
        self.assertFalse(kayip)

    def test_kismi_aktarim_kabul(self):
        # Tüm değerler gerekmez; biri geçse kural tetiklenmez.
        kayip, gerekce = dg.kayip_mi("Ödediğin tutar 4.960,50 TL.", OCR_KUCUK)
        self.assertFalse(kayip, gerekce)

    def test_kabaca_eslesme_kabul(self):
        # OCR '4.960,50' der, model '4.960' der → bilgi kaybı değil.
        kayip, _ = dg.kayip_mi("Tutar 4.960 TL.", OCR_KUCUK)
        self.assertFalse(kayip)

    def test_tek_haneli_eslesme_anlamsiz(self):
        # "50" gibi tek haneli eşleşmeyi saymıyoruz.
        self.assertFalse(dg._tasiyor_mu("Toplam 50 adet.", "4.960,50"))


class TestSoruYolu(unittest.TestCase):
    def test_soruda_gecen_deger_tutulur(self):
        # Kullanıcı somut soru sorduğunda model değeri verir (ölçüldü).
        kayip, gerekce = dg.kayip_mi("Toplam 4.960,50 TL.", OCR_KUCUK)
        self.assertFalse(kayip, gerekce)

    def test_kararli_soru_korumasi(self):
        # "Toplam" sorusunda toplam geçiyor → kural tetiklenmez.
        cevap = "Toplam ödenecek miktar 4.960,50 TL'dir."
        self.assertFalse(dg.kayip_mi(cevap, OCR_KUCUK)[0])


if __name__ == "__main__":
    unittest.main(verbosity=2)
