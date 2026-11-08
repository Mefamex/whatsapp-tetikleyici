#!/usr/bin/env python3
"""reply.py + sozdizimi /reply testleri.

Çalıştırma:
    python3 -m pytest tests/test_reply.py -v

Dosya yönetimi gerçek disk kullanmaz: BEKLEYEN_CEVAP_DOSYA env'i tmp'e
yönlendirilir. Amacı: numara atama/silme kuralları, /reply ayrıştırma,
komut gövdesindeki hata durumları.
"""

import os
import sys
import unittest
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

CMD_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "tetikleyici", "cmd"
)
sys.path.insert(0, CMD_DIR)

import reply  # noqa: E402
from sozdizimi import parse_cmd  # noqa: E402


class ReplyDosyaTestleri(unittest.TestCase):
    def setUp(self):
        self.tmp_dir = Path(self._tmp_kok()) / "reply_test"
        self.tmp_dir.mkdir(parents=True, exist_ok=True)
        self.dosya = self.tmp_dir / "bekleyen_cevaplar.txt"
        self.zaman = Path(str(self.dosya) + ".zaman")
        os.environ["BEKLEYEN_CEVAP_DOSYA"] = str(self.dosya)

    @staticmethod
    def _tmp_kok() -> str:
        import tempfile

        return tempfile.gettempdir()

    def tearDown(self):
        os.environ.pop("BEKLEYEN_CEVAP_DOSYA", None)
        for p in (self.dosya, self.zaman):
            try:
                p.unlink()
            except OSError:
                pass

    def test_en_kucuk_eksik(self):
        self.assertEqual(reply.en_kucuk_eksik([]), 0)
        self.assertEqual(reply.en_kucuk_eksik([1, 2, 4]), 3)
        self.assertEqual(reply.en_kucuk_eksik([2, 3]), 1)
        self.assertEqual(reply.en_kucuk_eksik([1, 2, 3, 4]), 5)
        self.assertEqual(reply.en_kucuk_eksik([0]), 1)

    def test_numara_ata_dosyaya_ekler(self):
        self.assertEqual(reply.numara_ata(), 0)  # boş → 0
        self.assertEqual(reply.bekleyenler(), [0])
        self.assertEqual(reply.numara_ata(), 1)
        self.assertEqual(reply.numara_ata(), 2)
        self.assertEqual(reply.bekleyenler(), [0, 1, 2])

    def test_numara_ata_eksigi_doldurur(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        reply.numara_ata()  # 2
        reply.numara_sil(1)
        self.assertEqual(reply.bekleyenler(), [0, 2])
        self.assertEqual(reply.numara_ata(), 1)  # eksik 1 doldurulur
        self.assertEqual(reply.bekleyenler(), [0, 1, 2])

    def test_numara_sil_yoksa_false(self):
        self.assertFalse(reply.numara_sil(99))
        reply.numara_ata()  # 0
        self.assertTrue(reply.numara_sil(0))
        self.assertEqual(reply.bekleyenler(), [])

    def test_dosya_chmod_600(self):
        reply.numara_ata()
        import stat

        mod = self.dosya.stat().st_mode & 0o777
        self.assertEqual(mod, 0o600)


class ReplyKomutTestleri(unittest.TestCase):
    def setUp(self):
        import tempfile

        tmp = Path(tempfile.gettempdir()) / "reply_komut_test"
        tmp.mkdir(parents=True, exist_ok=True)
        self.dosya = tmp / "bekleyen_cevaplar.txt"
        self.zaman = Path(str(self.dosya) + ".zaman")
        os.environ["BEKLEYEN_CEVAP_DOSYA"] = str(self.dosya)
        for p in (self.dosya, self.zaman):
            try:
                p.unlink()
            except OSError:
                pass

    def tearDown(self):
        os.environ.pop("BEKLEYEN_CEVAP_DOSYA", None)
        for p in (self.dosya, self.zaman):
            try:
                p.unlink()
            except OSError:
                pass

    def test_list_bos(self):
        self.assertIn("Bekleyen soru yok", reply.reply_komut("", "list"))

    def test_list_dolu(self):
        reply.numara_ata()
        reply.numara_ata()
        out = reply.reply_komut("", "list")
        self.assertIn("0", out)
        self.assertIn("1", out)

    def test_numarali_cevap_siler(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        out = reply.reply_komut("2. kez su ic", "1")
        self.assertIn("1 numaralı", out)
        self.assertIn("2. kez su ic", out)
        self.assertEqual(reply.bekleyenler(), [0])

    def test_numarasiz_cevap_0_gider(self):
        reply.numara_ata()  # 0
        out = reply.reply_komut("evet", "")
        self.assertIn("0 numaralı", out)
        self.assertEqual(reply.bekleyenler(), [])

    def test_bilinmeyen_numara(self):
        out = reply.reply_komut("cevap", "7")
        self.assertIn("yok", out)
        self.assertEqual(reply.bekleyenler(), [])

    def test_numarasiz_0_varken_0_gider(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        out = reply.reply_komut("cevap", "")
        self.assertIn("0 numaralı", out)
        self.assertEqual(reply.bekleyenler(), [1])

    def test_numarasiz_coklu_soru_0_yoksa_uyarir(self):
        # 0 silinmiş, 1 ve 2 bekliyor → numarasız cevap muğlak.
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        reply.numara_ata()  # 2
        reply.numara_sil(0)
        out = reply.reply_komut("cevap", "")
        self.assertIn("birden fazla", out)
        self.assertEqual(reply.bekleyenler(), [1, 2])


class ReplySozdizimiTestleri(unittest.TestCase):
    def test_parse_numarali(self):
        s = parse_cmd("/reply 3 cevap burada")
        self.assertEqual(s.komut_turu, "reply")
        self.assertEqual(s.numara, "3")
        self.assertEqual(s.metin, "cevap burada")

    def test_parse_numarasiz(self):
        s = parse_cmd("/reply cevap burada")
        self.assertEqual(s.komut_turu, "reply")
        self.assertEqual(s.numara, "")
        self.assertEqual(s.metin, "cevap burada")

    def test_parse_list(self):
        s = parse_cmd("/reply list")
        self.assertEqual(s.komut_turu, "reply")
        self.assertEqual(s.numara, "list")
        self.assertEqual(s.metin, "")

    def test_parse_buyuk_kucuk(self):
        s = parse_cmd("/REPLY 2 EVET")
        self.assertEqual(s.komut_turu, "reply")
        self.assertEqual(s.numara, "2")
        self.assertEqual(s.metin, "EVET")

    def test_diger_komutlar_etkilenmez(self):
        self.assertEqual(parse_cmd("/cmd echo merhaba").komut_turu, "once")
        self.assertEqual(parse_cmd("/zamanla 10dk su iç").komut_turu, "zamanla")
        self.assertEqual(parse_cmd("/durum").komut_turu, "durum")


class ReplyZamanAsimiTestleri(unittest.TestCase):
    """Bekleyen cevap 10 dk içinde kullanılmazsa serbest bırakılır."""

    def setUp(self):
        import tempfile

        tmp = Path(tempfile.gettempdir()) / "reply_zaman_test"
        tmp.mkdir(parents=True, exist_ok=True)
        self.dosya = tmp / "bekleyen_cevaplar.txt"
        self.zaman = Path(str(self.dosya) + ".zaman")
        os.environ["BEKLEYEN_CEVAP_DOSYA"] = str(self.dosya)
        for p in (self.dosya, self.zaman):
            try:
                p.unlink()
            except OSError:
                pass

    def tearDown(self):
        os.environ.pop("BEKLEYEN_CEVAP_DOSYA", None)
        for p in (self.dosya, self.zaman):
            try:
                p.unlink()
            except OSError:
                pass

    def test_numara_ata_damga_yazar(self):
        reply.numara_ata()  # 0
        damgalar = reply._damgalari_oku()
        self.assertIn(0, damgalar)
        simdi = int(reply.time.time())
        self.assertLessEqual(abs(damgalar[0] - simdi), 5)

    def test_bayat_temizlenir(self):
        reply.numara_ata()  # 0 → taze damga
        reply.numara_ata()  # 1 → taze damga
        # 0'ı bayatlaştır: 10 dk + 1 saat önceye çek.
        damgalar = reply._damgalari_oku()
        damgalar[0] = int(reply.time.time()) - reply.ZAMAN_ASIMI_SANIYE - 3600
        reply._damgalari_yaz(damgalar)

        silinen = reply._temizle()
        self.assertEqual(silinen, [0])
        self.assertEqual(reply.bekleyenler(), [1])
        self.assertNotIn(0, reply._damgalari_oku())

    def test_genc_temizlenmez(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        self.assertEqual(reply._temizle(), [])
        self.assertEqual(reply.bekleyenler(), [0, 1])

    def test_numara_ata_bayati_serbest_birakip_doldurur(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        reply.numara_ata()  # 2
        # 1'i bayatlaştır → serbest kalacak.
        damgalar = reply._damgalari_oku()
        damgalar[1] = int(reply.time.time()) - reply.ZAMAN_ASIMI_SANIYE - 3600
        reply._damgalari_yaz(damgalar)

        # Yeni atama: bayat 1 temizlenir, eksik 1 yeniden kullanılır.
        self.assertEqual(reply.numara_ata(), 1)
        self.assertEqual(reply.bekleyenler(), [0, 1, 2])

    def test_numara_sil_damgayi_da_siler(self):
        reply.numara_ata()  # 0
        reply.numara_sil(0)
        damgalar = reply._damgalari_oku()
        self.assertNotIn(0, damgalar)

    def test_reply_list_bayati_gostermez(self):
        reply.numara_ata()  # 0
        reply.numara_ata()  # 1
        damgalar = reply._damgalari_oku()
        damgalar[1] = int(reply.time.time()) - reply.ZAMAN_ASIMI_SANIYE - 3600
        reply._damgalari_yaz(damgalar)

        out = reply.reply_komut("", "list")
        self.assertIn("0", out)
        # Yeni format: "0: HH:MM — soru" — "1" substring olabilir (örn. "21:54" içinde)
        # Bu yüzden satır başı kontrolü yap: "1:" veya " 1 " olarak gelmemeli
        self.assertNotRegex(out, r"(^|\n)1:")  # "1:" satır başı yok
        self.assertNotRegex(out, r" 1 ")  # " 1 " arada yok
        self.assertEqual(reply.bekleyenler(), [0])

    def test_damgasiz_numara_silinmez(self):
        # Ana dosyada 7 var ama damgası yok (eski format / elle eklenmiş).
        self.dosya.write_text("7\n", encoding="ascii")
        self.assertEqual(reply._temizle(), [])
        self.assertEqual(reply.bekleyenler(), [7])


if __name__ == "__main__":
    unittest.main()
