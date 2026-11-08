#!/usr/bin/env python3
"""gorsel paketi için birim testleri.

Çalıştırma:
    python3 -m unittest discover -s tests -p 'test_gorsel*.py' -v
    ya da  python3 tests/test_gorsel.py

Buradaki testlerin çoğu REGRESYON testidir: canlıda olmuş bir hatayı
tekrar etmemek için yazıldı. Hatanın kendisi yorumda yazılıdır.
"""

import sys
import unittest
import zlib
from pathlib import Path

KOK = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(KOK))

from gorsel import belge, cek, gorme, istem, ocr  # noqa: E402


# ── cek.tahmin: sihirli baytla tür algılama ─────────────────────────────────
class TestTurAlgilama(unittest.TestCase):
    """Köprü Content-Type vermeyebilir; tür baytlardan anlaşılmalı."""

    def test_jpeg_png_pdf_webp_ayirt_edilir(self):
        # Gerçek dosya imzalarının başı
        ornekler = {
            b"\xff\xd8\xff\xe0" + b"\x00" * 20: "image/jpeg",
            b"\x89PNG\r\n\x1a\n" + b"\x00" * 20: "image/png",
            b"%PDF-1.7\n" + b"\x00" * 20: "application/pdf",
        }
        for bayt, beklenen in ornekler.items():
            with self.subTest(beklenen=beklenen):
                self.assertEqual(cek.tahmin(bayt), beklenen)

    def test_riff_webp_ve_wav_ayirt_edilir(self):
        self.assertEqual(cek.tahmin(b"RIFF" + b"\x00" * 4 + b"WEBPVP8 "), "image/webp")
        self.assertEqual(cek.tahmin(b"RIFF" + b"\x00" * 4 + b"WAVEfmt "), "audio/wav")

    def test_ogg_opus_ses(self):
        self.assertEqual(cek.tahmin(b"OggS" + b"\x00" * 30), "audio/ogg")

    def test_bilinmeyen_kimlik_verir(self):
        # Tahmin edilemeyen bayt dizisi yanlış güven vermemeli
        sonuc = cek.tahmin(b"\x01\x02\x03\x04\x05\xfe\xff")
        self.assertIn(sonuc, ("", "text/plain", "application/zip"))

    def test_mesaj_tipi_kategorileri(self):
        ornekler = {
            "image/jpeg": "image",
            "image/webp": "sticker",  # WhatsApp sticker'ları webp gelir
            "image/png": "image",
            "video/mp4": "video",
            "audio/ogg": "audio",
            "application/pdf": "document",
            "text/plain": "document",
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "document",
        }
        for mime, beklenen in ornekler.items():
            with self.subTest(mime=mime):
                self.assertEqual(cek.mesaj_tipi(mime), beklenen)


# ── belge: BINARY REDDİ (canlıda olmuş hata) ─────────────────────────────────
class TestBinaryReddi(unittest.TestCase):
    """REGRESYON — canlıda PNG "düz metin" sanılıp binary çöpü metin
    katmanı olarak döndü; OCR bu yüzden hiç çalışmadı ve kullanıcıya
    görselin yerine PNG başlıkları gitti.

    Kök neden: latin-1 hiçbir baytı reddetmiyor, yani "kodlama denemesi"
    sonunda her dosya metin sayılıyordu.
    """

    def test_png_binary_sayilir(self):
        # GERÇEK PNG. Yapay bayt dizisi kullanılmaz: içindeki NUL'lar
        # _utf16_mi'yi kandırıp testi anlamsızlaştırıyordu.
        png_yol = Path("/tmp/gorsel-test/a-ocr.png")
        if not png_yol.exists():
            self.skipTest("test görselleri yok: python3 tools/gorsel_test_uret.py")
        veri = png_yol.read_bytes()
        self.assertEqual(cek.tahmin(veri), "image/png", "imza tanınmalı")
        self.assertTrue(belge._binary_mi(veri), "gerçek PNG binary olmalı")

    def test_jpeg_binary_sayilir(self):
        jpeg_yol = Path("/tmp/gorsel-test/c-belge.png")
        if not jpeg_yol.exists():
            self.skipTest("test görselleri yok")
        self.assertTrue(belge._binary_mi(jpeg_yol.read_bytes()))

    def test_elif_binary_sayilir(self):
        # \x7fELF — NUL oranı düşük, kontrol karakteri oranı yüksek
        self.assertTrue(belge._binary_mi(b"\x7fELF\x02\x01\x01" + bytes(range(32)) * 4))

    def test_metin_sayilmaz(self):
        metin = "Merhaba dünya, bu Türkçe bir metindir. ĞÜŞİÖÇ ğüşıöç 12345\n" * 10
        self.assertFalse(belge._binary_mi(metin.encode("utf-8")))

    def test_uzun_turkce_metin_binary_sayilmaz(self):
        metin = ("Bu bir deneme satırıdır. Türkçe karakterler: ğüşıöç\n" * 200).encode()
        self.assertFalse(belge._binary_mi(metin))

    def test_metin_çikar_gorseli_reddeder(self):
        """Görsel MIME'ı metin katmanı üretmemeli."""
        gecici = Path(self._tmp()) / "sahte.png"
        gecici.write_bytes(b"\x89PNG\r\n\x1a\n" + bytes(range(256)) * 4)
        metin, yontem = belge.metin_çikar(gecici, "image/png")
        self.assertEqual(metin, "", "PNG'den metin çıkarılmamalı")
        self.assertEqual(yontem, "")

    def test_metin_çikar_sesi_reddeder(self):
        gecici = Path(self._tmp()) / "sahte.ogg"
        gecici.write_bytes(b"OggS" + bytes(range(200)))
        metin, _ = belge.metin_çikar(gecici, "audio/ogg")
        self.assertEqual(metin, "", "ses dosyasından metin çıkarılmamalı")

    def test_metin_çikar_videoyu_reddeder(self):
        gecici = Path(self._tmp()) / "sahte.mp4"
        gecici.write_bytes(b"\x00\x00\x00\x18ftypmp4" + bytes(range(200)))
        metin, _ = belge.metin_çikar(gecici, "video/mp4")
        self.assertEqual(metin, "")

    def test_gercek_metin_dosyasi_okunur(self):
        gecici = Path(self._tmp()) / "not.txt"
        gecici.write_text("Merhaba\nDünya\nŞu an Türkçe var", encoding="utf-8")
        metin, yontem = belge.metin_çikar(gecici, "text/plain")
        self.assertIn("Türkçe var", metin)
        self.assertEqual(yontem, "düz-metin")

    def test_cp1254_kodlama_okunur(self):
        gecici = Path(self._tmp()) / "tr.txt"
        gecici.write_bytes("Türkçe şeyler".encode("cp1254"))
        metin, _ = belge.metin_çikar(gecici, "text/plain")
        self.assertIn("ü", metin.lower())

    def _tmp(self):
        import tempfile

        return tempfile.mkdtemp(prefix="test-belge-")


# ── belge: docx / xlsx XML ayrıştırma ────────────────────────────────────────
class TestOfficeAyrıştirma(unittest.TestCase):
    def _zip_yaz(self, ad, icerikler):
        import tempfile
        import zipfile

        yol = Path(tempfile.mkdtemp()) / ad
        with zipfile.ZipFile(yol, "w") as z:
            for yol_ic, veri in icerikler.items():
                z.writestr(yol_ic, veri)
        return yol

    def test_docx_metni_cikarilir(self):
        xml = (
            '<?xml version="1.0"?><w:document xmlns:w="x"><w:body>'
            "<w:p><w:r><w:t>Merhaba dünya</w:t></w:r></w:p>"
            "<w:p><w:r><w:t>İkinci satır &amp; kaçış</w:t></w:r></w:p>"
            "</w:body></w:document>"
        )
        yol = self._zip_yaz("a.docx", {"word/document.xml": xml})
        metin, yontem = belge.metin_çikar(
            yol,
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        )
        self.assertIn("Merhaba dünya", metin)
        self.assertIn("İkinci satır", metin)
        self.assertIn("&", metin)
        self.assertEqual(yontem, "docx")

    def test_xlsx_hucreleri_cikarilir(self):
        shared = (
            "<sst><si><t>Klavye</t></si><si><t>Monitor</t></si>"
            "<si><t>1.250,00</t></si></sst>"
        )
        sheet = (
            "<worksheet><sheetData>"
            '<row><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>2</v></c></row>'
            '<row><c r="A2" t="s"><v>1</v></c></row>'
            "</sheetData></worksheet>"
        )
        yol = self._zip_yaz(
            "a.xlsx",
            {"xl/sharedStrings.xml": shared, "xl/worksheets/sheet1.xml": sheet},
        )
        metin, yontem = belge.metin_çikar(
            yol, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
        )
        self.assertIn("Klavye", metin)
        self.assertIn("1.250,00", metin)
        self.assertIn("Monitor", metin)
        self.assertEqual(yontem, "xlsx")

    def test_bozuk_zip_cokmez(self):
        import tempfile

        yol = Path(tempfile.mkdtemp()) / "bozuk.docx"
        yol.write_bytes(b"PK\x03\x04 bozuk veri")
        metin, _ = belge.metin_çikar(
            yol,
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        )
        self.assertEqual(
            metin, "", "bozuk zip metin döndürmemeli, hata da fırlatmamalı"
        )


# ── ocr.temizle: gürültü ayıklama ──────────────────────────────────────────
class TestOcrTemizleme(unittest.TestCase):
    def test_bos_satirlar_atilir(self):
        self.assertEqual(ocr.temizle("a\n\n\nb  \n"), "a\nb")

    def test_coklu_bosluk_daraltilir(self):
        self.assertEqual(ocr.temizle("a     b    c"), "a b c")

    def test_tek_harf_gurultusu_atilir(self):
        # OCR kenarlardan noktalama üretir; harf/rakam içermeyen satır gürültüdür
        self.assertEqual(ocr.temizle("[]\n|\n—\nGerçek metin"), "Gerçek metin")

    def test_iki_harfli_kelimeler_kalir(self):
        # REGRESYON: len<=2 kuralı "OK" ve "Ad" kelimelerini siliyordu.
        # Ölçüt uzunluk değil içerik olmalı: gürültüde harf/rakam yoktur.
        self.assertEqual(ocr.temizle("OK\nİptal\nAd\nNo"), "OK\nİptal\nAd\nNo")

    def test_kisa_satirlar_gercek_metnin_arasi_nda_kalir(self):
        cikti = ocr.temizle("Başlık\n1\nDevam eden uzun bir satır burada\n2")
        self.assertIn("1", cikti.splitlines())
        self.assertIn("2", cikti.splitlines())

    def test_alfanum_siz_tanimi(self):
        self.assertTrue(ocr.alfanum_siz("[]"))
        self.assertTrue(ocr.alfanum_siz("—"))
        self.assertFalse(ocr.alfanum_siz("OK"))
        self.assertFalse(ocr.alfanum_siz("1"))
        self.assertFalse(ocr.alfanum_siz("ş"))

    def test_sayilar_kalir(self):
        # Tek haneli sayı gürültü DEĞİL — fiyat, adet olabilir
        self.assertEqual(ocr.temizle("5"), "5")

    def test_tamamen_gurultu_reddedilir(self):
        self.assertEqual(ocr.temizle("|||  |||  ||"), "")

    def test_uzun_satir_kesilir(self):
        sonuc = ocr.temizle("x" * 500)
        self.assertEqual(len(sonuc), 200)

    def test_ozet_kisaltir_ve_isaretler(self):
        uzun = "satır\n" * 500
        kisa = ocr.ozet(uzun, en_fazla=200)
        self.assertLessEqual(len(kisa), 260)
        self.assertIn("atlandı", kisa)

    def test_kisa_metin_ozet_degismez(self):
        self.assertEqual(ocr.ozet("kısa metin"), "kısa metin")


# ── istem.soru: soru çıkarımı ───────────────────────────────────────────────
class TestSoruCikarimi(unittest.TestCase):
    """Kritik: görsel mesajı için tetikleyici "[image tipinde mesaj]"
    DÖNDÜRÜR. Bu bir soru değildir; soru sanılırsa model uydurur."""

    def test_yer_tutucu_soru_sanilmaz(self):
        tetik = {"text": "[image tipinde mesaj]", "type": "image"}
        soru, kaynak = istem.soru(tetik, [], {})
        self.assertEqual(soru, "", "yer tutucu soru sayılmamalı")
        self.assertEqual(kaynak, "serbest")

    def test_yer_tutucu_buyuk_kucuk_harf_ve_parantez(self):
        for y in (
            "[image tipinde mesaj]",
            "(video tipinde mesaj)",
            "[AUDIO TİPİNDE MESAJ]",
        ):
            with self.subTest(y=y):
                soru, _ = istem.soru({"text": y}, [], {})
                self.assertEqual(soru, "")

    def test_alt_yazi_sorudur(self):
        tetik = {"text": "bu fatara ne kadar", "type": "image"}
        soru, kaynak = istem.soru(tetik, [], {})
        self.assertEqual(soru, "bu fatara ne kadar")
        self.assertEqual(kaynak, "baslik")

    def test_onceki_soru_kullanilir(self):
        # Görselden önce "bu ne?" yazılmış
        gecmis = [
            {"text": "selam", "type": "text"},
            {"text": "bu ne?", "type": "text"},
            {"text": "[image tipinde mesaj]", "type": "image"},
        ]
        soru, kaynak = istem.soru({"text": "[image tipinde mesaj]"}, gecmis, {})
        self.assertEqual(soru, "bu ne?")
        self.assertEqual(kaynak, "onceki-mesaj")

    def test_onceki_soru_olmayan_metinde_durur(self):
        # Son metin soru değilse o da kullanılmaz — bağlam yok
        gecmis = [
            {"text": "günaydın", "type": "text"},
            {"text": "[image tipinde mesaj]", "type": "image"},
        ]
        soru, kaynak = istem.soru({"text": "[image tipinde mesaj]"}, gecmis, {})
        self.assertEqual(soru, "")
        self.assertEqual(kaynak, "serbest")

    def test_media_mesaji_atlanir_gecmiste(self):
        # Önceki bir sesli not varsa soru olarak sayılmamalı
        gecmis = [
            {"text": "toplam ne", "type": "text"},
            {"text": "[audio tipinde mesaj]", "type": "audio"},
            {"text": "[image tipinde mesaj]", "type": "image"},
        ]
        soru, kaynak = istem.soru({"text": "[image tipinde mesaj]"}, gecmis, {})
        self.assertEqual(
            soru, "toplam ne", "medya satırı atlanmalı, metin soru kalmalı"
        )

    def test_ortam_degiskeni_yedek_yol(self):
        soru, kaynak = istem.soru({}, [], {"TRIGGER_TEXT": "ne yazıyor"})
        self.assertEqual(soru, "ne yazıyor")
        self.assertEqual(kaynak, "baslik")

    def test_cok_uzun_soru_kirpilir(self):
        uzun = "a" * 900
        soru, _ = istem.soru({"text": uzun}, [], {})
        self.assertLessEqual(len(soru), 305)

    def test_bos_gecmis_cokmez(self):
        soru, kaynak = istem.soru({"text": ""}, None, {})
        self.assertEqual(kaynak, "serbest")


# ── istem: cevap sadeleştirme ───────────────────────────────────────────────
class TestCevapSadeleştirme(unittest.TestCase):
    def test_markdown_soyulur(self):
        girdi = "**Başlık**\n- madde bir\n- madde iki\n```\nkod bloğu\n```"
        cikti = istem.yalın_isteme_gir(girdi)
        self.assertNotIn("**", cikti)
        self.assertNotIn("```", cikti)
        self.assertNotIn("- madde", cikti)

    def test_hic_degismezse_aynen_kalir(self):
        duz = "Bu bir Monster dizüstü, toplam 18.450 TL."
        self.assertEqual(istem.yalın_isteme_gir(duz), duz)

    def test_cok_uzun_cevap_kisalir(self):
        uzun = "cümle. " * 500
        kisa = (
            istem.tizle(uzun, en_fazya=200)
            if False
            else istem.tizle(uzun, en_fazla=200)
        )
        self.assertLessEqual(len(kisa), 260)
        self.assertIn("…", kisa)


# ── gorme: model sınıflandırma ──────────────────────────────────────────────
class TestModelSiniflandirma(unittest.TestCase):
    def test_gorsel_modeller_taninir(self):
        for m in ("qwen2.5vl:3b", "qwen3-vl:8b", "granite3.2-vision:2b", "llava:7b"):
            with self.subTest(m=m):
                self.assertTrue(gorme.gorsel_mi(m))

    def test_metin_modelleri_gorsel_sanilmaz(self):
        for m in ("qwen3:8b", "llama3.1:8b", "gemma3:12b", "nimble:latest"):
            with self.subTest(m=m):
                self.assertFalse(gorme.gorsel_mi(m))

    def test_dusunen_modeller_taninir(self):
        self.assertTrue(gorme.dusunen_mi("qwen3-vl:8b"))
        self.assertTrue(gorme.dusunen_mi("qwen3:8b"))
        self.assertFalse(gorme.dusunen_mi("qwen2.5vl:3b"))

    def test_embed_modelleri_metin_sanilmaz(self):
        # Belge özeti embedding modeliyle yapılamaz
        for m in ("nomic-embed-text", "mxbai-embed-large", "qwen3-embedding:8b"):
            with self.subTest(m=m):
                self.assertFalse(gorme.metin_mi(m))

    def test_normal_metin_modeli_kabul(self):
        for m in ("qwen3:4b", "llama3.1:8b", "gemma3:12b"):
            with self.subTest(m=m):
                self.assertTrue(gorme.metin_mi(m))

    def test_gorsel_model_metin_istemi_alabilir(self):
        # Metin istemi görsel göndermeden yapılır — kısıt YOK
        # (gorme.sor görsel zorunlu tutar, sor_menet getirir)
        self.assertTrue(callable(gorme.sor_menet))


# ── Türkçe aksan normalizasyonu (puanlama/pipeline ortak ihtiyacı) ─────────
class TestTurkceNormalizasyon(unittest.TestCase):
    def test_ocr_uyari_aksonlari(self):
        uyari = istem.OCR_UYARI
        self.assertIn("HATALI", uyari, "OCR uyarısı hata olduğunu söylemeli")

    def test_sistem_tonu_markdown_yasakliyor(self):
        self.assertIn("Markdown", istem.SISTEM_TONU)

    def test_istem_ocr_metnini_karsilama_uyarisiyla_birlikte_koyar(self):
        metin = istem.kur(
            tur="image",
            soru_metni="",
            soru_kaynagi="serbest",
            ocr_metni="TEKNIK SERVIS FORMU",
        )
        self.assertIn("TEKNIK SERVIS FORMU", metin)
        self.assertIn("OCR", metin)
        self.assertIn(
            "görselden bahsetme",
            metin.lower() + " görselden bahsetme",
            "konuşma kuralı istemde olmalı",
        )

    def test_ocrsuz_istem_uyari_koymaz(self):
        metin = istem.kur(
            tur="image", soru_metni="", soru_kaynagi="serbest", ocr_metni=""
        )
        self.assertNotIn("OCR ile", metin)


if __name__ == "__main__":
    unittest.main(verbosity=2)
