#!/usr/bin/env python3
"""Sticker hattı regresyon testleri (gorsel/sticker.py).

Çalıştırma:
    python3 -m unittest discover -s tests -p 'test_sticker*' -v

Bu dosyadaki testler vision modeline BAĞIMLI DEĞİLDİR: `gorme.sor` ve
`ocr.calistir` mocklanır. Amaç mekanizmayı kilitlemek:

    webp → PNG (magick) → OCR önce → metin varsa OCR'lu vision istemi,
    metin yoksa düz sticker istemi.

CANLI ÖLÇÜM (2026-10-06 09:31): sentetik "MERHABA TEST" webp'i tam
zincirle 21,6 sn'de işlendi; OCR metni vision istemine taşındı.
"""

import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from gorsel import sticker, gorme, ocr  # noqa: E402

MAGICK = shutil.which("magick") or shutil.which("convert")


def webp_ornek() -> Path:
    """Sentetik, metinli bir webp üretir (magick gerekir)."""
    fd, yol = tempfile.mkstemp(suffix=".webp")
    os.close(fd)
    p = Path(yol)
    sonuc = subprocess.run(
        [
            MAGICK,
            "-background",
            "white",
            "-fill",
            "black",
            "-pointsize",
            "80",
            "-size",
            "600x400",
            "label:MERHABA TEST",
            str(p),
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    if sonuc.returncode != 0 or not p.exists() or p.stat().st_size == 0:
        raise unittest.SkipTest(f"webp üretilemedi: {sonuc.stderr[:200]}")
    return p


def animasyonlu_webp_ornek() -> Path:
    """İki kareli ANİMASYONLU webp üretir (canlı sticker tuzağını taklit).

    Dönüş 0 baytlık çıktı üreten webp'tir: ImageMagick animasyonlu webp'i
    JPG/PNG'ye yazarken rc=0 + 0 BAYT üretir (`[0]` ilk kare seçilmezse).
    Gerçek animasyon yazılamazsa test atlanır (magick derlemesine bağlı).
    """
    td = tempfile.mkdtemp(prefix="anim-webp-")
    try:
        kareler = [Path(td) / "k1.png", Path(td) / "k2.png"]
        for i, k in enumerate(kareler):
            renk = "red" if i == 0 else "blue"
            subprocess.run(
                [MAGICK, "-size", "64x64", f"xc:{renk}", str(k)],
                check=True,
                capture_output=True,
                text=True,
                timeout=30,
            )
        fd, yol = tempfile.mkstemp(suffix=".webp")
        os.close(fd)
        p = Path(yol)
        sonuc = subprocess.run(
            [
                MAGICK,
                "-delay",
                "10",
                "-loop",
                "0",
                str(kareler[0]),
                str(kareler[1]),
                str(p),
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
    finally:
        import shutil

        shutil.rmtree(td, ignore_errors=True)

    if sonuc.returncode != 0 or not p.exists() or p.stat().st_size == 0:
        raise unittest.SkipTest(f"animasyonlu webp üretilemedi: {sonuc.stderr[:200]}")
    # Gerçekten animasyonlu mu? (kare sayısı > 1 olmalı)
    kare_sayisi = 0
    tespit = subprocess.run(
        [MAGICK, "identify", str(p)], capture_output=True, text=True, timeout=30
    )
    if tespit.returncode == 0:
        kare_sayisi = len(tespit.stdout.splitlines())
    if kare_sayisi < 2:
        p.unlink(missing_ok=True)
        raise unittest.SkipTest(f"animasyonlu webp üretilemedi (kare={kare_sayisi})")
    return p


class TestWebpToPng(unittest.TestCase):
    """webp → PNG dönüşümü (magick)."""

    def setUp(self):
        if not sticker.MAGICK:
            self.skipTest("ImageMagick yok")

    def test_webp_png_donusumu(self):
        webp = webp_ornek()
        self.addCleanup(webp.unlink, missing_ok=True)
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as t:
            png = Path(t.name)
        self.addCleanup(png.unlink, missing_ok=True)

        self.assertTrue(sticker.webp_to_png(webp, png))
        self.assertTrue(png.exists())
        self.assertGreater(png.stat().st_size, 0)

    def test_varolmayan_dosya_false(self):
        # Varolmayan GİRİŞ → False, çıktı dosyası da oluşmaz.
        cikti = Path(tempfile.mktemp(suffix=".png"))
        self.addCleanup(cikti.unlink, missing_ok=True)
        self.assertFalse(sticker.webp_to_png(Path("/yok/boyle.webp"), cikti))
        self.assertFalse(cikti.exists())


class TestAnimasyonluWebp(unittest.TestCase):
    """CANLI REGRESYON (2026-10-06 23:28): sahibin 23 kareli sticker'ı
    "Görseli indirdim ama işleyemedim (görsel dönüştürülemedi: bilinmeyen
    hata)" ile düştü. Kök neden: animasyonlu webp → magick rc=0 + 0 BAYT
    dosya üretiyordu. `[0]` ilk kare seçimi + 0 bayt kontrolü çözer."""

    def setUp(self):
        if not sticker.MAGICK:
            self.skipTest("ImageMagick yok")

    def test_webp_to_png_animasyonlu_ilk_kare(self):
        webp = animasyonlu_webp_ornek()
        self.addCleanup(webp.unlink, missing_ok=True)
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as t:
            png = Path(t.name)
        self.addCleanup(png.unlink, missing_ok=True)

        self.assertTrue(sticker.webp_to_png(webp, png), "ilk kare çıkarılmalı")
        self.assertGreater(png.stat().st_size, 0, "0 bayt = boş dosya tuzağı")

    def test_sticker_isle_animasyonlu_ilk_kare_png(self):
        """Animasyonlu webp tam zincirden geçmeli; modele PNG (ilk kare) gitmeli."""
        webp = animasyonlu_webp_ornek()
        self.addCleanup(webp.unlink, missing_ok=True)

        with (
            mock.patch.object(ocr, "calistir", return_value=("", 3)),
            mock.patch.object(gorme, "sor", return_value="SAHNE CEVABI") as sor_mock,
            mock.patch.object(gorme, "gorsel_model_sec", return_value="fake-model"),
        ):
            cevap = sticker.sticker_isle(webp)

        self.assertEqual(cevap, "SAHNE CEVABI")
        self.assertEqual(sor_mock.call_count, 1)
        yol = sor_mock.call_args[0][1][0]
        self.assertEqual(yol.suffix.lower(), ".png", "modele ilk kare PNG gitmeli")


class TestStickerIsle(unittest.TestCase):
    """sticker_isle dalları: OCR'lu / düz vision / hata. Vision MOCK'lu."""

    def setUp(self):
        if not sticker.MAGICK:
            self.skipTest("ImageMagick yok")
        self.webp = webp_ornek()
        self.addCleanup(self.webp.unlink, missing_ok=True)

        # Vision her iki dala da mock'lu: çağrıyı yakala, sabit cevap dön.
        self.vision_cagri = {}
        self.sor_patch = mock.patch.object(
            gorme,
            "sor",
            side_effect=lambda model, yollar, ist: self._vision(model, yollar, ist),
        )
        self.model_patch = mock.patch.object(
            gorme, "gorsel_model_sec", return_value="fake-model"
        )

    def _vision(self, model, yollar, ist):
        self.vision_cagri["model"] = model
        self.vision_cagri["yollar"] = yollar
        self.vision_cagri["ist"] = ist
        return "SAHNE CEVABI"

    def test_metin_varsa_ocrli_istem_gider(self):
        """OCR metin bulursa vision isteminde o metin olmalı (OCR önce)."""
        with (
            mock.patch.object(ocr, "calistir", return_value=("MERHABA TEST", 3)),
            self.sor_patch,
            self.model_patch,
        ):
            cevap = sticker.sticker_isle(self.webp)

        self.assertEqual(cevap, "SAHNE CEVABI")
        ist = self.vision_cagri["ist"]
        self.assertIn("MERHABA TEST", ist, "OCR metni vision istemine taşınmalı")
        # Modele ilk karenin PNG'si gider (animasyonlu webp statik modele
        # belirsizdi — canlı 23 kareli sticker 0 bayt tuzağına çarpmıştı).
        self.assertTrue(self.vision_cagri["yollar"][0].suffix.lower() == ".png")
        self.assertEqual(self.vision_cagri["model"], "fake-model")

    def test_metin_yoksa_duz_sticker_istemi(self):
        """OCR boşsa düz sticker betimleme istemi gider; istemde OCR metni YOK."""
        with (
            mock.patch.object(ocr, "calistir", return_value=("", 3)),
            self.sor_patch,
            self.model_patch,
        ):
            cevap = sticker.sticker_isle(self.webp)

        self.assertEqual(cevap, "SAHNE CEVABI")
        ist = self.vision_cagri["ist"]
        self.assertIn("çıkartma (sticker)", ist, "düz sticker tonu kullanılmalı")
        self.assertNotIn("MERHABA TEST", ist)

    def test_webp_degilse_desteklemez(self):
        """webp dışı dosya → kısa Türkçe uyarı, vision çağrılmaz."""
        with (
            mock.patch.object(ocr, "calistir") as ocr_mock,
            mock.patch.object(gorme, "sor") as sor_mock,
        ):
            cevap = sticker.sticker_isle(Path("/tmp/ornek.png"))

        self.assertIn("Sadece .webp", cevap)
        ocr_mock.assert_not_called()
        sor_mock.assert_not_called()


if __name__ == "__main__":
    unittest.main()
