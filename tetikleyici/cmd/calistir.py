#!/usr/bin/env python3
"""calistir.py — Süreç başlatma, çıkış kodu, zaman aşımı.

Bu modül, shell komutlarını güvenli bir şekilde çalıştırır.
"""

from __future__ import annotations

import os
import shlex
import signal
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Optional


@dataclass
class CalistirSonuc:
    """Çalıştırma sonucu."""

    stdout: str
    stderr: str
    cikis_kodu: int
    zaman_asimi: bool = False
    hata: str = ""


VARSAYILAN_ZAMAN_ASIMI = 180  # 3 dakika
MAKS_CIKTI_BOYUTU = 50000  # 50KB stdout/stderr sınırı


def kabuk() -> str:
    """Komutun çalışacağı kabuk.

    `shell=True` verilse bile Python varsayılan olarak /bin/sh kullanır.
    Kullanıcı "kendi terminalim" dediği için giriş kabuğu (zsh) seçilir:
    zsh'ye özgü sözdizimi sh'da çalışmaz, komut sessizce bozulur.

    Sıra: $SHELL → zsh → sh.
    """
    shell = os.environ.get("SHELL", "").strip()
    if shell and Path(shell).exists():
        return shell
    zsh = Path("/usr/bin/zsh")
    if zsh.exists():
        return str(zsh)
    return "/bin/sh"


def calistir(
    kod: str,
    zaman_asimi: int = VARSAYILAN_ZAMAN_ASIMI,
    calisma_dizini: Optional[str] = None,
    ortam: Optional[dict] = None,
    debug_cikti: bool = False,
) -> CalistirSonuc:
    """Shell komutunu çalıştır ve sonucu döndür.

    Args:
        kod: Çalıştırılacak shell komutu
        zaman_asimi: Saniye cinsinden zaman aşımı (0 = sınırsız)
        calisma_dizini: Çalışma dizini (None = geçerli dizin)
        ortam: Ek ortam değişkenleri (os.environ ile birleştirilir)
        debug_cikti: true ise ortam değişkeni DEBUG_CIKTI=true eklenir

    Returns:
        CalistirSonuc: stdout, stderr, çıkış kodu, zaman aşımı bilgisi
    """
    # Çalışma dizini
    if calisma_dizini is None:
        calisma_dizini = str(Path.cwd())

    # Ortam değişkenleri
    env = os.environ.copy()
    if ortam:
        env.update(ortam)
    if debug_cikti:
        env["DEBUG_CIKTI"] = "true"

    # Zaman aşımı 0 ise None yap (sınırsız)
    timeout = zaman_asimi if zaman_asimi > 0 else None

    try:
        # Popen ile başlat (stdout/stderr yakalama için)
        proses = subprocess.Popen(
            kod,
            shell=True,
            executable=kabuk(),
            cwd=calisma_dizini,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,  # Satır tamponlu
            preexec_fn=os.setsid,  # Yeni süreç grubu (kill için)
        )

        # Zaman aşımı için timer
        timer = None
        if timeout:

            def kill_proses():
                try:
                    os.killpg(os.getpgid(proses.pid), signal.SIGTERM)
                    time.sleep(0.5)
                    # Hala hayatsa SIGKILL
                    try:
                        os.killpg(os.getpgid(proses.pid), signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                except ProcessLookupError:
                    pass

            timer = threading.Timer(timeout, kill_proses)
            timer.start()

        try:
            stdout, stderr = proses.communicate(timeout=timeout)
            zaman_asimi_oldu = False
        except subprocess.TimeoutExpired:
            zaman_asimi_oldu = True
            stdout, stderr = "", ""
            # communicate timeout'ta proses hala yaşıyor, kill edilecek
            try:
                os.killpg(os.getpgid(proses.pid), signal.SIGTERM)
                time.sleep(0.5)
                try:
                    os.killpg(os.getpgid(proses.pid), signal.SIGKILL)
                except ProcessLookupError:
                    pass
            except ProcessLookupError:
                pass
            # Son çıktıyı almaya çalış
            try:
                stdout, stderr = proses.communicate(timeout=1)
            except subprocess.TimeoutExpired:
                stdout, stderr = "", ""

        finally:
            if timer:
                timer.cancel()

        # Çıktı boyutunu sınırla
        if stdout and len(stdout) > MAKS_CIKTI_BOYUTU:
            stdout = (
                stdout[:MAKS_CIKTI_BOYUTU]
                + f"\n... [çıktı kırpıldı, {len(stdout)} karakter]"
            )
        if stderr and len(stderr) > MAKS_CIKTI_BOYUTU:
            stderr = (
                stderr[:MAKS_CIKTI_BOYUTU]
                + f"\n... [stderr kırpıldı, {len(stderr)} karakter]"
            )

        cikis_kodu = proses.returncode if proses.returncode is not None else -1

        if zaman_asimi_oldu:
            return CalistirSonuc(
                stdout=stdout,
                stderr=stderr,
                cikis_kodu=cikis_kodu,
                zaman_asimi=True,
                hata=f"Zaman aşımı ({zaman_asimi} sn)",
            )

        return CalistirSonuc(
            stdout=stdout,
            stderr=stderr,
            cikis_kodu=cikis_kodu,
        )

    except Exception as e:
        return CalistirSonuc(
            stdout="",
            stderr="",
            cikis_kodu=-1,
            hata=f"Başlatma hatası: {e}",
        )


def calistir_ve_yazdir(
    kod: str,
    zaman_asimi: int = VARSAYILAN_ZAMAN_ASIMI,
    calisma_dizini: Optional[str] = None,
    ortam: Optional[dict] = None,
    stdout_callback=None,
    stderr_callback=None,
    debug_cikti: bool = False,
) -> CalistirSonuc:
    """Komutu çalıştır ve çıktıları anlık callback'lerle işle.

    Uzun süren komutlarda stdout/stderr'i anlık almak için.
    """
    if calisma_dizini is None:
        calisma_dizini = str(Path.cwd())

    env = os.environ.copy()
    if ortam:
        env.update(ortam)
    if debug_cikti:
        env["DEBUG_CIKTI"] = "true"

    timeout = zaman_asimi if zaman_asimi > 0 else None

    stdout_parcalar = []
    stderr_parcalar = []

    def oku_ve_callback(pipe, buffer_list, callback):
        for line in iter(pipe.readline, ""):
            buffer_list.append(line)
            if callback:
                callback(line)

    try:
        proses = subprocess.Popen(
            kod,
            shell=True,
            executable=kabuk(),
            cwd=calisma_dizini,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            bufsize=1,
            preexec_fn=os.setsid,
        )

        # Okuma thread'leri
        stdout_thread = threading.Thread(
            target=oku_ve_callback,
            args=(proses.stdout, stdout_parcalar, stdout_callback),
        )
        stderr_thread = threading.Thread(
            target=oku_ve_callback,
            args=(proses.stderr, stderr_parcalar, stderr_callback),
        )
        stdout_thread.start()
        stderr_thread.start()

        # Zaman aşımı timer
        timer = None
        if timeout:

            def kill_proses():
                try:
                    os.killpg(os.getpgid(proses.pid), signal.SIGTERM)
                    time.sleep(0.5)
                    try:
                        os.killpg(os.getpgid(proses.pid), signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                except ProcessLookupError:
                    pass

            timer = threading.Timer(timeout, kill_proses)
            timer.start()

        try:
            proses.wait(timeout=timeout)
            zaman_asimi_oldu = False
        except subprocess.TimeoutExpired:
            zaman_asimi_oldu = True
            try:
                os.killpg(os.getpgid(proses.pid), signal.SIGTERM)
                time.sleep(0.5)
                try:
                    os.killpg(os.getpgid(proses.pid), signal.SIGKILL)
                except ProcessLookupError:
                    pass
            except ProcessLookupError:
                pass

        finally:
            if timer:
                timer.cancel()

        stdout_thread.join(timeout=2)
        stderr_thread.join(timeout=2)

        stdout = "".join(stdout_parcalar)
        stderr = "".join(stderr_parcalar)

        if stdout and len(stdout) > MAKS_CIKTI_BOYUTU:
            stdout = stdout[:MAKS_CIKTI_BOYUTU] + f"\n... [çıktı kırpıldı]"
        if stderr and len(stderr) > MAKS_CIKTI_BOYUTU:
            stderr = stderr[:MAKS_CIKTI_BOYUTU] + f"\n... [stderr kırpıldı]"

        cikis_kodu = proses.returncode if proses.returncode is not None else -1

        if zaman_asimi_oldu:
            return CalistirSonuc(
                stdout=stdout,
                stderr=stderr,
                cikis_kodu=cikis_kodu,
                zaman_asimi=True,
                hata=f"Zaman aşımı ({zaman_asimi} sn)",
            )

        return CalistirSonuc(
            stdout=stdout,
            stderr=stderr,
            cikis_kodu=cikis_kodu,
        )

    except Exception as e:
        return CalistirSonuc(
            stdout="",
            stderr="",
            cikis_kodu=-1,
            hata=f"Başlatma hatası: {e}",
        )


if __name__ == "__main__":
    # Test
    print("Test 1: Basit echo")
    sonuc = calistir("echo merhaba dünya")
    print(f"  stdout: {sonuc.stdout.strip()}")
    print(f"  stderr: {sonuc.stderr.strip()}")
    print(f"  kod: {sonuc.cikis_kodu}")

    print("\nTest 2: Zaman aşımı (sleep 5, timeout 1)")
    sonuc = calistir("sleep 5 && echo bitti", zaman_asimi=1)
    print(f"  zaman_asimi: {sonuc.zaman_asimi}")
    print(f"  hata: {sonuc.hata}")

    print("\nTest 3: Hatalı komut")
    sonuc = calistir("olmayan_komut_xyz")
    print(f"  kod: {sonuc.cikis_kodu}")
    print(f"  stderr: {sonuc.stderr.strip()[:100]}")
