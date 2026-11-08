#!/usr/bin/env python3
"""voice_pipeline.transcribe — GPU OOM'da CPU'ya düşme regresyon testleri.

Çalıştırma:
    python3 -m pytest tests/test_voice_pipeline.py -v

ÖLÇÜLEN HATA (2026-10-14 08:12:55 ve 08:38:50, iki ayrı sesli not):
GPU başka bir iş tarafından doluyken faster-whisper modeli yüklenemedi:

    RuntimeError: CUDA failed with error out of memory

Sesli not tamamen düşüyordu ("sesli not çözülemedi" → kullanıcıya
"İş tamamlanamadı"). Düzeltme: CUDA denemesi patlarsa CPU/int8 ile bir
kez daha denenir. Bu testler o davranışı kilitler.
"""

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

KOK = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(KOK))

import voice_pipeline as vp  # noqa: E402


def _proc(returncode=0, stdout="[]", stderr=""):
    """subprocess.run dönüşünü taklit eder (CompletedProcess yeter)."""
    return mock.Mock(returncode=returncode, stdout=stdout, stderr=stderr)


class TranskriptFallbackTestleri(unittest.TestCase):
    def setUp(self):
        # Gerçek venv/model/ffmpeg'e dokunmadan yalnız cihaz seçimini sınarız.
        self.patches = [
            mock.patch.object(vp, "venv_python", return_value="/fake/python"),
            mock.patch.object(vp, "model_dir", return_value=Path("/fake/model")),
            mock.patch.object(vp, "asr_env", return_value={}),
        ]
        for p in self.patches:
            p.start()
            self.addCleanup(p.stop)

    def _cihazlar(self, run_mock):
        """subprocess.run'a geçen komutlardaki device=... değerlerini toplar."""
        cihazlar = []
        for cagri in run_mock.call_args_list:
            script = cagri.args[0][-1]  # [-c, script]
            for cihaz in ("cuda", "cpu"):
                if f'device="{cihaz}"' in script:
                    cihazlar.append(cihaz)
        return cihazlar

    def test_gpu_basariliysa_tek_deneme(self):
        with (
            mock.patch.object(vp, "DEVICE", "cuda"),
            mock.patch.object(
                vp.subprocess, "run", return_value=_proc(stdout='[{"text":"merhaba"}]')
            ) as run,
        ):
            sonuc = vp.transcribe("/tmp/x.wav")

        self.assertEqual(sonuc, [{"text": "merhaba"}])
        self.assertEqual(run.call_count, 1)
        self.assertEqual(self._cihazlar(run), ["cuda"])

    def test_gpu_oom_ise_cpu_ile_yeniden_dener(self):
        hatali = _proc(
            returncode=1,
            stderr="... RuntimeError: CUDA failed with error out of memory",
        )
        basarili = _proc(stdout='[{"text":"kurtuldu"}]')
        with (
            mock.patch.object(vp, "DEVICE", "cuda"),
            mock.patch.object(
                vp.subprocess, "run", side_effect=[hatali, basarili]
            ) as run,
        ):
            sonuc = vp.transcribe("/tmp/x.wav")

        self.assertEqual(sonuc, [{"text": "kurtuldu"}])
        self.assertEqual(run.call_count, 2)
        self.assertEqual(self._cihazlar(run), ["cuda", "cpu"])
        # CPU denemesi int8 kullanmalı (int8_float16 CPU'da geçersiz).
        ikinci = run.call_args_list[1].args[0][-1]
        self.assertIn('compute_type="int8"', ikinci)

    def test_birincil_cpu_ise_tekrar_denemez(self):
        with (
            mock.patch.object(vp, "DEVICE", "cpu"),
            mock.patch.object(
                vp.subprocess, "run", return_value=_proc(returncode=1, stderr="boom")
            ) as run,
        ):
            with self.assertRaises(RuntimeError):
                vp.transcribe("/tmp/x.wav")

        self.assertEqual(run.call_count, 1)

    def test_gpu_disi_hata_yukari_cikar(self):
        # Örn. model bulunamadı: CPU'da da bulunamaz, ikinci deneme anlamsız.
        hatali = _proc(returncode=1, stderr="model bulunamadı")
        with (
            mock.patch.object(vp, "DEVICE", "cuda"),
            mock.patch.object(vp.subprocess, "run", return_value=hatali) as run,
        ):
            with self.assertRaises(RuntimeError):
                vp.transcribe("/tmp/x.wav")

        # CUDA başarısız olunca yine de CPU denenir (hata türünü ayrıştırmıyoruz),
        # ikinci deneme de patlar ve hata yukarı çıkar.
        self.assertEqual(run.call_count, 2)


if __name__ == "__main__":
    unittest.main()
