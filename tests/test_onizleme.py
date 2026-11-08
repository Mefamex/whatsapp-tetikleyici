#!/usr/bin/env python3
"""onizleme.py hazirla regresyon testleri.

Çalıştırma:
    python3 -m unittest discover -s tests -p 'test_onizleme*' -v

CANLI REGRESYON (2026-10-06 23:28): sahibin 23 kareli animasyonlu sticker'ı
"görsel dönüştürülemedi: bilinmeyen hata" ile düştü. ImageMagick animasyonlu
webp'i JPG/PNG'ye yazarken rc=0 ile 0 BAYTLIK dosya üretiyordu; bu "başarı"
sayılıp modele boş dosya gidiyordu.

İki düzeltme:
    1. Girdiye `[0]` (ilk kare) — animasyonda ilk kare alınır.
    2. 0 baytlık çıktı artık HATA sayılır ("üretilen dosya boş").
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

from gorsel import onizleme  # noqa: E402

MAGICK = shutil.which("magick") or shutil.which("convert")


def animasyonlu_webp() -> Path:
    """İki kareli animasyonlu webp üretir. Üretilemezse SkipTest."""
    td = tempfile.mkdtemp(prefix="anim-webp-")
    try:
        kareler = [Path(td) / "k1.png", Path(td) / "k2.png"]
        for i, k in enumerate(kareler):
            subprocess.run(
                [MAGICK, "-size", "64x64", f"xc:{'red' if i == 0 else 'blue'}", str(k)],
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
        shutil.rmtree(td, ignore_errors=True)

    kare = 0
    if sonuc.returncode == 0 and p.exists() and p.stat().st_size > 0:
        t = subprocess.run(
            [MAGICK, "identify", str(p)], capture_output=True, text=True, timeout=30
        )
        if t.returncode == 0:
            kare = len(t.stdout.splitlines())
    if kare < 2:
        p.unlink(missing_ok=True)
        raise unittest.SkipTest(f"animasyonlu webp üretilemedi (kare={kare})")
    return p


class TestHazirlaAnimasyonluWebp(unittest.TestCase):
    def setUp(self):
        if not onizleme.IMAGEMAGICK:
            self.skipTest("ImageMagick yok")

    def test_hazirla_animasyonlu_webp_ilk_kare(self):
        """Animasyonlu webp → hazirla: 0 bayt DEĞİL gerçek JPEG üretmeli."""
        webp = animasyonlu_webp()
        self.addCleanup(webp.unlink, missing_ok=True)
        hedef = Path(tempfile.mktemp(suffix=".jpg"))
        self.addCleanup(hedef.unlink, missing_ok=True)

        onizleme.hazirla(webp, hedef)  # hata fırlatmamalı
        self.assertTrue(hedef.exists())
        self.assertGreater(
            hedef.stat().st_size, 0, "animasyonlu webp 0 bayt üretmemeli"
        )

    def test_hazirla_0_bayt_hata_sayar(self):
        """rc=0 ama çıktı dosyası YOK/BOŞ ise OnIslemeHatasi fırlatılmalı.

        Eski davranış bunu başarı sayıyordu (rc kontrolü + exists kontrolü
        vardı ama boyut kontrolü YOKTU) — canlı sticker bununla düştü.
        """
        hedef = Path(tempfile.mktemp(suffix=".jpg"))
        self.addCleanup(hedef.unlink, missing_ok=True)
        sahte = mock.Mock()
        sahte.returncode = 0
        sahte.stderr = ""
        with mock.patch.object(onizleme.subprocess, "run", return_value=sahte):
            with self.assertRaises(onizleme.OnIslemeHatasi) as cm:
                onizleme.hazirla(Path("/tmp/varolmayan.webp"), hedef)
        self.assertIn("görsel dönüştürülemedi", str(cm.exception))
        self.assertIn("0 bayt", str(cm.exception), "boş dosya nedeni açık olmalı")


if __name__ == "__main__":
    unittest.main()
