#!/usr/bin/env python3
"""zamanla.py — /zamanla aç|kapat (MEVZU Aşama 3, ADIM 2) testleri.

Çalıştırma:
    python3 -m pytest tests/test_zamanla.py -v

Config dosyası gerçek tetikleyici.yaml'a DOKUNMAZ: TRIGGER_CONFIG env'i tmp'e
yönlendirilir. Amacı: `acik:` satırının atomik değişimi (diğer yapılar
korunur), blok yoksa ekleme, ve kapalıyken /zamanla aç komutunun çalışması.
"""

import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

CMD_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "tetikleyici", "cmd"
)
sys.path.insert(0, CMD_DIR)

import zamanla  # noqa: E402


class ZamanlaAcKapatTestleri(unittest.TestCase):
    def setUp(self):
        kok = Path(tempfile.gettempdir()) / "zamanla_test"
        kok.mkdir(parents=True, exist_ok=True)
        self.cfg = kok / "tetikleyici.yaml"
        self.dizin = kok / "zamanlanmis"
        os.environ["TRIGGER_CONFIG"] = str(self.cfg)
        os.environ["ZAMANLANMIS_DIZIN"] = str(self.dizin)

    def tearDown(self):
        for anahtar in ("TRIGGER_CONFIG", "ZAMANLANMIS_DIZIN", "ZAMANLANMIS_ACIK"):
            os.environ.pop(anahtar, None)

    def _yaz(self, icerik: str) -> None:
        self.cfg.write_text(icerik, encoding="utf-8")

    def _oku(self) -> str:
        return self.cfg.read_text(encoding="utf-8")

    def test_ac_ayarla_canliyken_acik_satirini_true_yapar(self):
        # Aşama 3 senaryosu: yalnız 1+2+3(ezen) — kullanıcı çalışma anında açar.
        self._yaz(
            "# AÇIK OLAN ÖZELLİKLER bloğu\n"
            "zamanlanmis_mesaj:\n"
            "  acik: false\n"
            "ai_eti_ekle: false\n"
        )
        cevap = zamanla._ac_ayarla(True)
        self.assertIn("AÇILDI", cevap)
        icerik = self._oku()
        self.assertIn("  acik: true", icerik)
        self.assertNotIn("  acik: false", icerik)
        # Diğer yapılar ve yorumlar korunmalı.
        self.assertIn("# AÇIK OLAN ÖZELLİKLER bloğu", icerik)
        self.assertIn("ai_eti_ekle: false", icerik)

    def test_ac_ayarla_kapatirken_false_yazar(self):
        self._yaz("zamanlanmis_mesaj:\n  acik: true\n")
        cevap = zamanla._ac_ayarla(False)
        self.assertIn("KAPATILDI", cevap)
        self.assertIn("  acik: false", self._oku())

    def test_ac_ayarla_blok_yoksa_ekler(self):
        self._yaz("ai_eti_ekle: false\n")
        zamanla._ac_ayarla(True)
        icerik = self._oku()
        self.assertIn("zamanlanmis_mesaj:", icerik)
        self.assertIn("  acik: true", icerik)

    def test_ac_ayarla_acik_satiri_yoksa_blok_icine_ekler(self):
        self._yaz(
            "zamanlanmis_mesaj:\n  # acik yorum satiri\nnotify:\n  on_start: false\n"
        )
        zamanla._ac_ayarla(True)
        self.assertIn("  acik: true", self._oku())

    def test_zamanla_komut_kapaliyken_ac_komudu_calisir(self):
        # ZAMANLANMIS_ACIK=0 (kapalı) ama /zamanla aç yine de açmalı.
        os.environ["ZAMANLANMIS_ACIK"] = "0"
        self._yaz("zamanlanmis_mesaj:\n  acik: false\n")
        sonuc = zamanla.zamanla_komut("aç", "", "905551234567@s.whatsapp.net")
        self.assertIn("AÇILDI", sonuc)
        self.assertIn("  acik: true", self._oku())

    def test_zamanla_komut_kapat_komudu_calisir(self):
        os.environ["ZAMANLANMIS_ACIK"] = "1"
        self._yaz("zamanlanmis_mesaj:\n  acik: true\n")
        sonuc = zamanla.zamanla_komut("kapat", "", "905551234567@s.whatsapp.net")
        self.assertIn("KAPATILDI", sonuc)
        self.assertIn("  acik: false", self._oku())

    def test_zamanla_komut_kapaliyken_list_kapali_der(self):
        os.environ["ZAMANLANMIS_ACIK"] = "0"
        self._yaz("zamanlanmis_mesaj:\n  acik: false\n")
        sonuc = zamanla.zamanla_komut("list", "", "905551234567@s.whatsapp.net")
        self.assertIn("kapalı", sonuc)
        self.assertIn("/zamanla aç", sonuc)


if __name__ == "__main__":
    unittest.main()


class ZamanlaIdTestleri(unittest.TestCase):
    def setUp(self):
        kok = Path(tempfile.gettempdir()) / "zamanla_id_test"
        self.dizin = kok / "zamanlanmis"
        self.dizin.mkdir(parents=True, exist_ok=True)
        for eski in self.dizin.glob("*.json"):
            eski.unlink()
        os.environ["ZAMANLANMIS_DIZIN"] = str(self.dizin)
        os.environ["ZAMANLANMIS_ACIK"] = "1"

    def tearDown(self):
        for anahtar in ("ZAMANLANMIS_DIZIN", "ZAMANLANMIS_ACIK"):
            os.environ.pop(anahtar, None)
        for eski in self.dizin.glob("*.json"):
            eski.unlink()

    def test_bos_dizinde_ilk_id_1(self):
        self.assertEqual(zamanla._yeni_id(self.dizin), 1)

    def test_arti_arda_sirali_verir(self):
        (self.dizin / "1.json").write_text("{}", encoding="utf-8")
        (self.dizin / "2.json").write_text("{}", encoding="utf-8")
        self.assertEqual(zamanla._yeni_id(self.dizin), 3)

    def test_bosluk_doldurulur(self):
        (self.dizin / "1.json").write_text("{}", encoding="utf-8")
        (self.dizin / "3.json").write_text("{}", encoding="utf-8")
        self.assertEqual(zamanla._yeni_id(self.dizin), 2)

    def test_eski_nano_id_varsa_yine_kisa_verir(self):
        (self.dizin / "1791325000000000000.json").write_text("{}", encoding="utf-8")
        self.assertEqual(zamanla._yeni_id(self.dizin), 1)

    def test_sayi_olmayan_dosya_atlanir(self):
        (self.dizin / "not.json").write_text("{}", encoding="utf-8")
        self.assertEqual(zamanla._yeni_id(self.dizin), 1)

    def test_zamanla_komut_kisa_id_doner(self):
        cevap = zamanla.zamanla_komut("10dk", "deneme mesaji", "905551234567@s.whatsapp.net")
        self.assertIn("/zamanla iptal 1", cevap)
        self.assertTrue((self.dizin / "1.json").exists())
