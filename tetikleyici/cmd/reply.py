#!/usr/bin/env python3
"""reply.py — /reply komutu: bekleyen soru cevapları.

Sahibin tasarımı (TODO.md §/reply, 2026-10-06 05:40): cevaplar
`.tetikleyici/bekleyen_cevaplar.txt` dosyasında, satır başına bir sayı
(ASCII, LF). Akış:

    Soru sorulduğunda (ajan tarafı, ADIM 2):
        dosyayı oku → en küçük eksik pozitif sayı (1,2,4 → 3)
        dosya boşsa → 0
        sayıyı dosyaya ekle + metne "> cevaplamak için /reply veya /reply 3"

    Cevap geldiğinde (bu modül):
        /reply 3 cevap → 3'ü sil (dosya yeniden yazılır)
        /reply cevap   → 0'ı sil (numarasız)
        /reply list    → bekleyenleri göster

    Zaman aşımı (2026-10-06): atanan numara 10 dk içinde kullanılmazsa
    serbest bırakılır. Damga dosyası <ana>.zaman (numara epoch) Go
    reply.go ile ORTAKTIR; süreyi aşan numara hem ana hem damga
    dosyasından silinir. Uyarı yalnız günlüğe yazılır — kullanıcıya
    WhatsApp hatırlatması GİTMEZ. Damgası olmayan (eski format)
    numaralara dokunulmaz.

YENİ (2026-10-14): Soru metinleri `.tetikleyici/bekleyen_sorular.json` dosyasında
saklanır: {numara: {soru, zaman, jid}} — /reply list'te gösterilir.

ADIM 1: komut + dosya yönetimi. Ajan entegrasyonu (soru anında numara
atama + cevabın ajan oturumuna iletilmesi) ayrı adım — burada sadece
kayıt mekanizması.
"""

from __future__ import annotations

import json
import os
import time
from pathlib import Path

# Zaman aşımı: bekleyen bir cevap numarası 10 dk içinde kullanılmazsa serbest
# bırakılır (TODO §/reply). Uyarı yalnız günlüğe gider — unutan kullanıcıya
# WhatsApp hatırlatması spam olur, /reply list zaten günceli gösterir.
ZAMAN_ASIMI_SANIYE = 10 * 60


def _dosya_yolu() -> Path:
    """Bekleyen cevap dosyası: env'den, yoksa proje .tetikleyici/."""
    if d := os.environ.get("BEKLEYEN_CEVAP_DOSYA", "").strip():
        return Path(d)
    return (
        Path(__file__).resolve().parents[2] / ".tetikleyici" / "bekleyen_cevaplar.txt"
    )


def _sorular_yolu() -> Path:
    """Soru metinleri JSON dosyası: numara -> {soru, zaman, jid}."""
    return Path(str(_dosya_yolu()) + ".sorular.json")


def _zaman_yolu() -> Path:
    """Damga dosyası: ana dosyanın yanında, '<ana>.zaman' eki ile.

    Biçim Go reply.go ile ORTAKTIR: satır başına 'numara <epoch>' (ASCII, LF).
    """
    return Path(str(_dosya_yolu()) + ".zaman")


def _damgalari_oku() -> dict[int, int]:
    """Damga dosyasındaki numara → epoch (saniye) eşlemesi."""
    yol = _zaman_yolu()
    if not yol.is_file():
        return {}
    damgalar: dict[int, int] = {}
    try:
        satirlar = yol.read_text(encoding="ascii", errors="ignore").splitlines()
    except OSError:
        return {}
    for satir in satirlar:
        parca = satir.split()
        if len(parca) != 2 or not parca[0].isdigit() or not parca[1].isdigit():
            continue
        damgalar[int(parca[0])] = int(parca[1])
    return damgalar


def _damgalari_yaz(damgalar: dict[int, int]) -> None:
    """Damga dosyasını atomik yazar (tmp + rename, chmod 600)."""
    yol = _zaman_yolu()
    yol.parent.mkdir(parents=True, exist_ok=True)
    tmp = Path(str(yol) + ".tmp")
    satirlar = [f"{n} {damgalar[n]}" for n in sorted(damgalar)]
    tmp.write_text("\n".join(satirlar) + ("\n" if satirlar else ""), encoding="ascii")
    try:
        tmp.chmod(0o600)
    except OSError:
        pass
    tmp.replace(yol)


def _sorulari_oku() -> dict[int, dict]:
    """Soru metinleri JSON dosyasını okur."""
    yol = _sorular_yolu()
    if not yol.is_file():
        return {}
    try:
        return json.loads(yol.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return {}


def _sorulari_yaz(sorular: dict[int, dict]) -> None:
    """Soru metinleri JSON dosyasını atomik yazar."""
    yol = _sorular_yolu()
    yol.parent.mkdir(parents=True, exist_ok=True)
    tmp = Path(str(yol) + ".tmp")
    tmp.write_text(json.dumps(sorular, ensure_ascii=False, indent=2), encoding="utf-8")
    try:
        tmp.chmod(0o600)
    except OSError:
        pass
    tmp.replace(yol)


def _temizle() -> list[int]:
    """Süresi dolan bekleyen numaraları hem ana hem damga hem soru dosyasından siler.

    Dönüş: silinen numaralar (günlük için). Damgası olmayan numaralara
    dokunulmaz — yaşı bilinmeyeni silmek yanlış pozitif olur.
    """
    damgalar = _damgalari_oku()
    if not damgalar:
        return []
    esik = int(time.time()) - ZAMAN_ASIMI_SANIYE
    bayat = sorted(n for n, e in damgalar.items() if e < esik)
    if not bayat:
        return []
    bayat_set = set(bayat)
    sayilar = [s for s in bekleyenler() if s not in bayat_set]
    _yaz(sayilar)
    for n in bayat:
        damgalar.pop(n, None)
    _damgalari_yaz(damgalar)
    # Soru metinlerini de temizle
    sorular = _sorulari_oku()
    for n in bayat:
        sorular.pop(str(n), None)
    _sorulari_yaz(sorular)
    return bayat


def bekleyenler() -> list[int]:
    """Dosyadaki bekleyen cevap numaraları (sıralı, benzersiz)."""
    yol = _dosya_yolu()
    if not yol.is_file():
        return []
    sayilar: list[int] = []
    try:
        satirlar = yol.read_text(encoding="ascii", errors="ignore").splitlines()
    except OSError:
        return []
    for satir in satirlar:
        satir = satir.strip()
        if satir.isdigit():
            sayilar.append(int(satir))
    return sorted(set(sayilar))


def _yaz(sayilar: list[int]) -> None:
    """Dosyayı sayılarla yeniden yazar (atomik: tmp + rename, chmod 600)."""
    yol = _dosya_yolu()
    yol.parent.mkdir(parents=True, exist_ok=True)
    tmp = yol.with_suffix(".tmp")
    tmp.write_text(
        "\n".join(map(str, sayilar)) + ("\n" if sayilar else ""), encoding="ascii"
    )
    try:
        tmp.chmod(0o600)
    except OSError:
        pass
    tmp.replace(yol)


def en_kucuk_eksik(sayilar: list[int]) -> int:
    """1'den başlayan ilk boşluk; liste boşsa 0 (sahip kuralı).

    [1,2,4] → 3 · [2,3] → 1 · [1,2,3,4] → 5 · [] → 0
    """
    if not sayilar:
        return 0
    onceki = 0
    for s in sayilar:
        if s - onceki > 1:
            return onceki + 1
        onceki = s
    return onceki + 1


def _format_zaman(epoch: int) -> str:
    """Epoch'ı HH:MM formatına çevirir."""
    from datetime import datetime

    return datetime.fromtimestamp(epoch).strftime("%H:%M")


def numara_ata(soru: str = "", jid: str = "") -> int:
    """Bekleyen listedeki ilk boş numarayı dosyaya ekler ve döndürür.

    Önce süresi dolan bekleyenler serbest bırakılır (boşalan numara yeniden
    kullanılabilir); sonra numara hem ana hem damga hem soru dosyasına yazılır.
    """
    _temizle()
    sayilar = bekleyenler()
    num = en_kucuk_eksik(sayilar)
    _yaz(sorted(sayilar + [num]))
    now = int(time.time())
    damgalar = _damgalari_oku()
    damgalar[num] = now
    _damgalari_yaz(damgalar)
    # Soru metnini kaydet
    sorular = _sorulari_oku()
    sorular[str(num)] = {
        "soru": soru[:200] if soru else "",  # max 200 karakter
        "zaman": now,
        "jid": jid,
    }
    _sorulari_yaz(sorular)
    return num


def numara_sil(num: int) -> bool:
    """Numarayı dosyadan siler; damga kaydını ve soru metnini de temizler. Yoka false."""
    sayilar = bekleyenler()
    if num not in sayilar:
        return False
    _yaz([s for s in sayilar if s != num])
    damgalar = _damgalari_oku()
    if num in damgalar:
        damgalar.pop(num)
        _damgalari_yaz(damgalar)
    sorular = _sorulari_oku()
    if str(num) in sorular:
        sorular.pop(str(num))
        _sorulari_yaz(sorular)
    return True


def reply_komut(metin: str, numara: str) -> str:
    """/reply gövdesi. metin = cevap; numara = '3' | 'list' | '' (numarasız).

    ADIM 1'de cevap yalnızca kaydedilir (dosyadan silinir) ve onay döner;
    ajan oturumuna iletim ADIM 2'de eklenir.
    """
    _temizle()  # bayat bekleyenler listeyi şişirmesin (zaman aşımı)
    bekleyen = bekleyenler()
    sorular = _sorulari_oku()

    if numara == "list":
        if not bekleyen:
            return "Bekleyen soru yok."
        satirlar = []
        for n in bekleyen:
            s = sorular.get(str(n), {})
            soru_metni = s.get("soru", "")
            zaman = s.get("zaman", 0)
            if zaman:
                zaman_str = _format_zaman(zaman)
                if soru_metni:
                    satirlar.append(f"{n}: {zaman_str} — {soru_metni}")
                else:
                    satirlar.append(f"{n}: {zaman_str}")
            else:
                satirlar.append(str(n))
        return "Bekleyen sorular:\n" + "\n".join(satirlar)

    # Numarasız /reply: 0'a gider; 0 yoksa tek kayıt varsa onu alır.
    hedef = int(numara) if numara.isdigit() else None
    if hedef is None:
        if 0 in bekleyen:
            hedef = 0
        elif len(bekleyen) == 1:
            hedef = bekleyen[0]
        else:
            return (
                "Numarasız cevap için birden fazla bekleyen soru var.\n"
                "Hangisine cevap verdiğini belirt: /reply <numara> <cevap>\n"
                "Bekleyenler: " + (", ".join(map(str, bekleyen)) if bekleyen else "yok")
            )

    if not numara_sil(hedef):
        return f"{hedef} numaralı bekleyen soru yok. /reply list ile kontrol et."

    cevap_metni = metin.strip()
    return f"✅ {hedef} numaralı soruya cevap kaydedildi.\nCevap: {cevap_metni}"


if __name__ == "__main__":
    # Test modu: python3 reply.py "<metin>" "<numara>"
    import sys

    arglar = sys.argv[1:]
    if arglar:
        print(reply_komut(arglar[0], arglar[1] if len(arglar) > 1 else ""))
    else:
        print("kullanım: reply.py '<cevap>' '<numara|list|>'")
