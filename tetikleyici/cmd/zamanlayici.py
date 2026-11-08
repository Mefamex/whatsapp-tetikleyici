#!/usr/bin/env python3
"""zamanlayici.py — Saniyelik tekrar, 'kaldığı yerden devam'.

Tekrarlayan /cmd komutlarını yönetir. Her turda çıktıyı dosyaya yazar
ve WhatsApp'a gönderir. Durdurma sinyali ile güvenli kapanır.
"""

from __future__ import annotations

import atexit
import json
import os
import signal
import subprocess
import sys
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Callable, Optional

# Proje modülleri
sys.path.insert(0, str(Path(__file__).resolve().parent))
from calistir import calistir, CalistirSonuc
from cikti import yaz, CIKTI_KLASORU


@dataclass
class TekrarlayanGorev:
    """Bir tekrarlayan görev."""

    id: int
    kod: str
    aralik_saniye: int
    baslangic_zamani: float
    son_calisma: float = 0
    calisma_sayisi: int = 0
    aktif: bool = True
    dosya_adi: str = ""
    _durdur_bayragi: threading.Event = field(default_factory=threading.Event)
    _thread: Optional[threading.Thread] = None
    _child_pid: Optional[int] = (
        None  # çalışan child süreç PID'i (doğrudan öldürmek için)
    )


# Görev kayıt dosyası
GOREV_DOSYASI = CIKTI_KLASORU / ".gorevler.json"

# Kilit için
_gorev_kilidi = threading.Lock()
_gorevler: dict[int, TekrarlayanGorev] = {}
_son_id = 0


def _gorevleri_yukle() -> None:
    """Kayıtlı görevleri dosyadan yükle."""
    global _son_id
    if not GOREV_DOSYASI.exists():
        return
    try:
        with open(GOREV_DOSYASI, "r", encoding="utf-8") as f:
            veri = json.load(f)
        for g_veri in veri.get("gorevler", []):
            gorev = TekrarlayanGorev(
                id=g_veri["id"],
                kod=g_veri["kod"],
                aralik_saniye=g_veri["aralik_saniye"],
                baslangic_zamani=g_veri["baslangic_zamani"],
                son_calisma=g_veri.get("son_calisma", 0),
                calisma_sayisi=g_veri.get("calisma_sayisi", 0),
                aktif=g_veri.get("aktif", True),
                dosya_adi=g_veri.get("dosya_adi", ""),
            )
            if gorev.aktif:
                _gorevler[gorev.id] = gorev
                if gorev.id > _son_id:
                    _son_id = gorev.id
    except Exception:
        pass  # Bozuk dosya, sıfırdan başla


def _gorevleri_kaydet() -> None:
    """Aktif görevleri dosyaya kaydet."""
    try:
        veri = {
            "gorevler": [
                {
                    "id": g.id,
                    "kod": g.kod,
                    "aralik_saniye": g.aralik_saniye,
                    "baslangic_zamani": g.baslangic_zamani,
                    "son_calisma": g.son_calisma,
                    "calisma_sayisi": g.calisma_sayisi,
                    "aktif": g.aktif,
                    "dosya_adi": g.dosya_adi,
                }
                for g in _gorevler.values()
            ]
        }
        with open(GOREV_DOSYASI, "w", encoding="utf-8") as f:
            json.dump(veri, f, ensure_ascii=False, indent=2)
    except Exception:
        pass


def _gorev_calistir(
    gorev: TekrarlayanGorev, cikti_callback: Optional[Callable[[str], None]] = None
) -> None:
    """Görevin tek bir turunu çalıştır. Child PID'yi takip eder."""
    if not gorev.aktif or gorev._durdur_bayragi.is_set():
        return

    # Çıktı dosyası adı (ilk turda oluştur, sonrakilere ekle)
    if not gorev.dosya_adi:
        gorev.dosya_adi = (
            f"cmd_{gorev.id}_{datetime.now().strftime('%Y%m%d_%H%M%S')}.txt"
        )

    # Komutu Popen ile başlat (process group için preexec_fn=os.setsid)
    # Child PID'yi gorev._child_pid'ye yazacağız
    zaman_asimi = (
        gorev.aralik_saniye - 5 if gorev.aralik_saniye > 10 else gorev.aralik_saniye
    )
    kabuk_bin = "/usr/bin/zsh" if Path("/usr/bin/zsh").exists() else "/bin/sh"

    proses = subprocess.Popen(
        gorev.kod,
        shell=True,
        executable=kabuk_bin,
        cwd=str(Path.cwd()),
        env=os.environ.copy(),
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
        preexec_fn=os.setsid,
    )

    # Child PID'yi kaydet (process group leader = proses.pid)
    gorev._child_pid = proses.pid

    try:
        stdout, stderr = proses.communicate(timeout=zaman_asimi)
        zaman_asimi_oldu = False
    except subprocess.TimeoutExpired:
        zaman_asimi_oldu = True
        stdout, stderr = "", ""
        # Timeout: child process group'ünü öldür
        try:
            os.killpg(os.getpgid(proses.pid), signal.SIGTERM)
            time.sleep(0.5)
            try:
                os.killpg(os.getpgid(proses.pid), signal.SIGKILL)
            except ProcessLookupError:
                pass
        except ProcessLookupError:
            pass
        try:
            stdout, stderr = proses.communicate(timeout=1)
        except subprocess.TimeoutExpired:
            stdout, stderr = "", ""

    # Child PID temizle (süreç bitti)
    gorev._child_pid = None

    gorev.son_calisma = time.time()
    gorev.calisma_sayisi += 1

    # Çıktıyı birleştir
    cikti_parcalar = []
    cikti_parcalar.append(
        f"=== Tur {gorev.calisma_sayisi} - {datetime.now().strftime('%H:%M:%S')} ==="
    )
    cikti_parcalar.append(f"Komut: {gorev.kod}")
    cikti_parcalar.append(
        f"Çıkış kodu: {proses.returncode if proses.returncode is not None else -1}"
    )
    if zaman_asimi_oldu:
        cikti_parcalar.append(f"⚠ Zaman aşımı ({gorev.aralik_saniye} sn)")
    if stdout:
        cikti_parcalar.append(f"--- STDOUT ---\n{stdout.strip()}")
    if stderr:
        cikti_parcalar.append(f"--- STDERR ---\n{stderr.strip()}")

    cikti = "\n".join(cikti_parcalar) + "\n"

    # Dosyaya yaz (ekle modunda)
    yaz(cikti, dosya_adi=gorev.dosya_adi, ekle=True)

    # Callback varsa çağır (WhatsApp'a gönderim için)
    if cikti_callback:
        satirlar = cikti.strip().split("\n")
        gonderilecek = (
            "\n".join(satirlar[-20:]) if len(satirlar) > 20 else cikti.strip()
        )
        cikti_callback(gonderilecek)

    _gorevleri_kaydet()


def _gorev_dongusu(
    gorev: TekrarlayanGorev, cikti_callback: Optional[Callable[[str], None]] = None
) -> None:
    """Görev ana döngüsü."""
    # İlk çalışmayı hemen yap
    _gorev_calistir(gorev, cikti_callback)

    while gorev.aktif and not gorev._durdur_bayragi.is_set():
        # Bekle (aralık saniye kadar, ama erken çıkış için 0.5sn ara ile kontrol)
        bekleme = gorev.aralik_saniye
        while bekleme > 0 and not gorev._durdur_bayragi.is_set():
            time.sleep(min(0.5, bekleme))
            bekleme -= 0.5

        if gorev._durdur_bayragi.is_set():
            break

        _gorev_calistir(gorev, cikti_callback)

    # Bittiğinde dosyaya "done" yaz
    if gorev.dosya_adi:
        bitis_mesaji = f"\n=== cmd done (PID {os.getpid()}, süre {int(time.time() - gorev.baslangic_zamani)} sn, {gorev.calisma_sayisi} tur) ===\n"
        yaz(bitis_mesaji, dosya_adi=gorev.dosya_adi, ekle=True)
        if cikti_callback:
            cikti_callback("cmd done")


def gorev_baslat(
    kod: str,
    aralik_saniye: int,
    cikti_callback: Optional[Callable[[str], None]] = None,
) -> TekrarlayanGorev:
    """Yeni tekrarlayan görev başlat."""
    global _son_id
    with _gorev_kilidi:
        _son_id += 1
        gorev = TekrarlayanGorev(
            id=_son_id,
            kod=kod,
            aralik_saniye=aralik_saniye,
            baslangic_zamani=time.time(),
        )
        _gorevler[_son_id] = gorev

        # Arka plan thread'inde başlat
        gorev._thread = threading.Thread(
            target=_gorev_dongusu,
            args=(gorev, cikti_callback),
            daemon=True,
        )
        gorev._thread.start()

        _gorevleri_kaydet()
        return gorev


def gorev_durdur(gorev_id: int) -> bool:
    """Belirli ID'li görevi durdur. Child PID varsa doğrudan öldür."""
    with _gorev_kilidi:
        gorev = _gorevler.get(gorev_id)
        if not gorev:
            return False
        gorev.aktif = False
        gorev._durdur_bayragi.set()
        # Child PID varsa doğrudan öldür (process group = child PID)
        if gorev._child_pid:
            try:
                os.killpg(os.getpgid(gorev._child_pid), signal.SIGTERM)
                time.sleep(0.5)
                try:
                    os.killpg(os.getpgid(gorev._child_pid), signal.SIGKILL)
                except ProcessLookupError:
                    pass
            except ProcessLookupError:
                pass
        if gorev._thread:
            gorev._thread.join(timeout=5)
        del _gorevler[gorev_id]
        _gorevleri_kaydet()
        return True


def tum_gorevleri_durdur() -> int:
    """Tüm aktif görevleri durdur. Child PID'leri doğrudan öldür."""
    with _gorev_kilidi:
        sayac = 0
        for gorev in list(_gorevler.values()):
            gorev.aktif = False
            gorev._durdur_bayragi.set()
            if gorev._child_pid:
                try:
                    os.killpg(os.getpgid(gorev._child_pid), signal.SIGTERM)
                    time.sleep(0.5)
                    try:
                        os.killpg(os.getpgid(gorev._child_pid), signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                except ProcessLookupError:
                    pass
            if gorev._thread:
                gorev._thread.join(timeout=5)
            sayac += 1
        _gorevler.clear()
        _gorevleri_kaydet()
        return sayac


def gorev_listele() -> list[dict]:
    """Aktif görevleri listele."""
    with _gorev_kilidi:
        return [
            {
                "id": g.id,
                "kod": g.kod[:80] + ("..." if len(g.kod) > 80 else ""),
                "aralik_saniye": g.aralik_saniye,
                "calisma_sayisi": g.calisma_sayisi,
                "son_calisma_sn_once": int(time.time() - g.son_calisma)
                if g.son_calisma > 0
                else -1,
                "dosya_adi": g.dosya_adi,
            }
            for g in sorted(_gorevler.values(), key=lambda x: x.id)
        ]


def gorev_getir(gorev_id: int) -> Optional[TekrarlayanGorev]:
    """ID ile görev getir."""
    with _gorev_kilidi:
        return _gorevler.get(gorev_id)


def kapanista_temizle() -> None:
    """Program kapanırken tüm görevleri durdur."""
    tum_gorevleri_durdur()


# Modül yüklendiğinde görevleri yükle ve çıkışta temizle
_gorevleri_yukle()
atexit.register(kapanista_temizle)


# Sinyal yakalayıcılar
def _sinyal_isleyici(signum, frame):
    kapanista_temizle()
    sys.exit(0)


signal.signal(signal.SIGTERM, _sinyal_isleyici)
signal.signal(signal.SIGINT, _sinyal_isleyici)


if __name__ == "__main__":
    # Test
    def test_callback(mesaj: str):
        print(f"[CALLBACK] {mesaj[:100]}")

    print("Test: 3 saniyede bir 'echo test' (2 tur)")
    gorev = gorev_baslat("echo 'tur $RANDOM'", 3, test_callback)
    print(f"Görev başlatıldı: ID={gorev.id}")

    time.sleep(8)
    print("Durduruluyor...")
    gorev_durdur(gorev.id)
    print("Bitti")

    print("\nAktif görevler:", gorev_listele())
