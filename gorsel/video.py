#!/usr/bin/env python3
"""gorsel/video.py — Video → ara kareler → vision betimleme.  [KLON]

⛔⛔ BU DOSYA ÜRETİM YOLU DEĞİLDİR. 2026-10-06'da ölçüldü.

Üretimde WhatsApp'tan gelen videoyu `gorsel_pipeline.py` işler
(`--tur video`, tetikleyicinin `allow_gorsel` eylemi). Bu dosyayı kimse
çağırmıyor: ne `tetikleyici.yaml`'da, ne `gorsel_pipeline.py`'de, ne
README'de geçmiyor. Yalnız elle çalıştırılabilir bir betik.

Bunu bilerek yazıyorum çünkü BUGÜN tam olarak burada ölçüm yaptım:
"video hattı çalışmıyor" sonucuna vardım, 224 saniyelik bir hatanın
peşinden koştum ve düzelttim — düzelttiğim şey üretimde çalışmayan bir
klonmuş. Asıl hata (`gorsel_pipeline.py` üç kareden yalnız birini
anlatıyordu) bu düzeltmeden sonra da duruyordu.

Kural: bir hattı ölçmeden önce ÖNCE o hattın gerçekten çağrıldığını
doğrula. `grep` tek satır cevap verir:
    grep -rn "gorsel.video" --include="*.py" --include="*.yaml" .

Bu klonun üretimdeki karşılığı olan iki davranış, orada FARKLI çalışıyor:
  · OCR: burada hiç çalıştırılmaz; üretimde video için de çalıştırılmıyor
    (bkz. gorsel_pipeline._ocr_gerekli)
  · kare betimleme: burada kare BAŞINA ayrı istek; üretimde ÜÇ KARE TEK
    istekte — ölçüldü: tek istek üçünü de doğru görüyor, daha hızlı.
    Bu yüzden "kare kare ayrı istek" yöntemi burada doğru, üretimde
    gereksiz maliyet olurdu; kopyalamayın.

Akış (elle çalıştırma için):
    video indir → ffprobe (süre) → ffmpeg (eşit aralıklı kareler)
    → her kare JPEG → onizleme (boyut/re-kod) → gorme (vision)
    → istem (betimleme istemi) → çıktı

Çıktı sözleşmesi: stdout DOLU olmak zorunda. Hata olsa bile Türkçe
açıklama yazar; boş stdout tetikleyiciyi "çıktı boş" yoluna düşürür.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import List, Optional

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from gorsel import onizleme, gorme, istem

# Sabitler
FFMPEG = shutil.which("ffmpeg")
FFPROBE = shutil.which("ffprobe")

# ⛔ BURADAN 3 SABİT SİLİNDİ: MAKS_KENAR, KARE_SURESI, MAKS_KARE.
# Üçü de ÖLÜDÜ — bu dosyada hiç okunmuyorlardı:
#   · MAKS_KENAR  → onizleme.py kendi MAKS_KENAR'ını kullanıyor
#     (ölçü: grep "video.py:31" tek satır; başka kullanım yok)
#   · KARE_SURESI → _kareleri_betimle kendi satırında yeniden okuyordu (300)
#   · MAKS_KARE   → çağıranlar `adet=3` geçiyor, sabit hiç okunmuyor
#
# Ölü sabit neden tehlikelidir: bir modülün sabitine bakan kişi "video
# kareleri 1600 px'e küçültülür, kare başına 30 sn süre var" diye düşünür.
# İkisi de YANLIŞ: küçültme onizleme.py'de 1600 (aynı sayı, tesadüfen), süre
# 300. Karışık iki ayrı varsayılan (30 ve 300) aynı adda yan yana duruyordu;
# hangisi geçerli, dosyayı okumadan anlaşılamıyordu. Ölü sabit, yaşayan
# sabitten daha çok yanıltır — çünkü dokunulduğu izlenimi verir.
#
# Yerine tek sabit geldi. Adı "kare süresi" değil "kare ÇAĞRISI süresi":
# ölçtüğümüz şey kare başına değil, modele yapılan her çağrının duvar
# saati süresidir.
KARE_CAGRI_SURESI = int(os.getenv("GORSEL_KARE_CAGRI_SURESI", "120"))

# Varsayılan 120 sn NEREDEN? Ölçüm (2026-10-06, aynı makine):
#   qwen2.5vl:3b tek kare   32,0 sn   ← bu dosyanın seçtiği model
#   qwen2.5vl:7b tek kare   49,5 sn   (kıyaslama tablosundan)
# 120 sn, ölçülen en yavaş görsel modele ~2,4 kat tam sayı pay bırakır.
# Tavan olarak 300 sn seçilmedi: 300 sn'yi aşan ollama çağrıları servisi
# kilitliyor (daha önce yaşandı) ve bu süre sonunda hata dönecekse
# kullanıcı 2 dakika bekleyip yine hata alır. 30 sn ise kesinlikle yanlış:
# ölçülen 32 sn'nin altında, yani model her karede zaman aşımına uğrardı.

BOS_MESAJ = "Video aldım ama betimleyemedim."


def log(mesaj: str) -> None:
    print(f"[video] {mesaj}", file=sys.stderr, flush=True)


# --- Hazır cevaplar (modelden önce, kullanıcıya hemen) ---


def indirilemedi() -> str:
    return "Videoyu indiremedim, dosya bozuk geldi ya da köprüye ulaşılamadı. Bir daha göndermeyi dene."


def vision_yok() -> str:
    return (
        "Videodan kareler aldım ama bu bilgisayarda görsel okuyan model yok, "
        "bu yüzden anlayamadım.\n\n"
        "Kurulum:  ollama pull "
        + (gorme.VARSAYILAN_MODELLER[0] if gorme.VARSAYILAN_MODELLER else "qwen3-vl:8b")
        + "\nSonra tekrar gönder."
    )


def onisleme_hatasi(hata: Exception) -> str:
    return f"Videodan kareler aldım ama işleyemedim ({hata}). Farklı bir dosya dene."


def beklenmeyen(hata: Exception) -> str:
    return f"Video işlerken hata oldu ({type(hata).__name__}). Ayrıntı tetikleyici günlüğünde."


# --- Ana akış ---


def _kareleri_betimle(kareler: list[Path]) -> list[str]:
    """Her kareyi vision modeline tek tek betimletir.

    Tek karenin hatası tüm videoyu öldürmez: başarısız kare atlanır,
    stderr'e yazılır. Hepsi başarısızsa ModelYok/GormeHatasi yükselir.

    Süre `KARE_CAGRI_SURESI` (120 sn): ölçülen tek kare süresi 32 sn,
    bkz. sabitin tanımı. Süre burada sabit olarak verilir, `gorme.sor`
    varsayılanını kullanılmaz — varsayılan (`SORU_SURESI` 240 sn) üç kare
    için 12 dakika demek, oysa hata durumunda beklemek en pahalı şeydir.
    """
    model = gorme.gorsel_model_sec()  # ModelYok yukarı yayılır: OCR-only moda düşülür.
    sonuclar = []
    for i, kare in enumerate(kareler, 1):
        kare_istemi = (
            istem.SISTEM_TONU + "\n\n"
            f"Videodan alınmış {i}. kare bu. Karede ne görülüyor, "
            "kısaca ve somut anlat (kim/kimler, ne yapıyor, ortam)."
        )
        try:
            sonuclar.append(gorme.sor(model, [kare], kare_istemi, KARE_CAGRI_SURESI))
        except gorme.GormeHatasi as e:
            # Kare atlanır, kalan karelerle devam edilir. Kullanıcıya
            # eksik kare bildirilir mi? Hayır — betimleme zaten kareye
            # özgü, kayıp kare diğerlerinin değerini düşürmez; gürültü
            # yaratır. Gövde çağıranın sorumluluğunda (video_betimle).
            log(f"kare {i} atlandı: {e}")
    if not sonuclar:
        raise gorme.GormeHatasi(
            f"hiçbir kare betimlenemedi ({len(kareler)} kare denendi)"
        )
    return sonuclar


def _ozetle(sonuclar: list[str]) -> str:
    """Kare betimlemelerini tek video özetine indirir (metin modeli).

    ⛔ ÖZETLEME BAŞARISIZ OLURSA SONUÇ ATILMAZ.
    Özetleme bir SÜS adımı: kare betimlemeleri elde edildikten sonra
    tek cümleye indirmek güzellik, işin kendisi değil.

    Ölçülen olay (2026-10-06): kare betimlemeleri BAŞARILI oldu, sonra
    özetleme modeli boş döndü ve `video_betimle` bu hatayı yakalayıp
    kullanıcıya "Videodan kareler aldım ama işleyemedim" dedi. 224 saniye
    sonra, elimizde 3 kare betimlemesi varken, kullanıcıya HİÇBİR ŞEY
    verilmedi.

    Kırık olan kısım değildi: kırık olan, bir sü adımının başarısızlığının
    tüm sonucu götürmesidir. Sü adımı başarısız olabilir; sonuç yine de
    vardır.
    """
    if len(sonuclar) == 1:
        prompt = istem.video_tek_kare(sonuclar[0])
    else:
        prompt = istem.video_coklu_kare(sonuclar)

    try:
        model = gorme.metin_model_sec()
    except gorme.ModelYok as e:
        log(f"özetleme yapılamadı ({e}); kareler ayrı veriliyor")
        return _ozetsiz(sonuclar)

    tam_istem = istem.SISTEM_TONU + "\n\n" + prompt
    try:
        ozet = gorme.sor_menet(model, tam_istem)
    except gorme.GormeHatasi as e:
        # Kare betimlemeleri elimizde; sonucu çöpe atmıyoruz.
        log(f"özetleme modeli boş döndü ({e}); kareler ayrı veriliyor")
        return _ozetsiz(sonuclar)

    ozet = ozet.strip()
    if not ozet:
        return _ozetsiz(sonuclar)
    return ozet


def _ozetsiz(sonuclar: list[str]) -> str:
    """Özetlenemediğinde kare betimlemelerini olduğu gibi kullanıcıya verir.

    Kullanıcıya eksik olduğunu SÖYLER. Sessizce daha kötü bir cevap
    vermek, tam vermemekten iyi değildir — ama hiç vermemek de özet
    yapamamaktan iyidir. İkisi arasında dürüst olan budur.
    """
    govde = "\n\n".join(f"{i}. {s}" for i, s in enumerate(sonuclar, 1))
    return "Videoyu kare kare inceledim, tek cümleye indiremedim:\n\n" + govde


def video_betimle(dosya: Path) -> str:
    """Yerel video dosyasından betimleme üretir (köprüsüz test için)."""
    import tempfile as _tf

    with _tf.TemporaryDirectory(prefix="video-kare-") as tmpdir:
        try:
            kareler = onizleme.kare_cikar(dosya, Path(tmpdir), adet=3)
        except onizleme.OnIslemeHatasi as e:
            return onisleme_hatasi(e)
        if not kareler:
            return "Videodan kare alınamadı (ffmpeg/ffprobe yok mu?)."

        try:
            sonuclar = _kareleri_betimle(kareler)
        except gorme.ModelYok:
            return vision_yok()
        except gorme.GormeHatasi as e:
            return onisleme_hatasi(e)

        # _ozetle artık GormeHatasi YUTMAZ: özetlenemezse kare betimlemelerini
        # kendisi döndürür. Buradaki eski "except → onisleme_hatasi" dalı
        # ölü koddu ve 2026-10-06'da tam da bu yüzden 3 kare betimlemesi
        # elimizdeyken kullanıcıya hiçbir şey verilmemişti.
        cevap = _ozetle(sonuclar)
        return cevap.strip() if cevap else "Kareler alındı ama betimleme üretilmedi."


def video_betimle_bridge(media_id: str, bridge_base: str) -> str:
    """Köprüden video indir → kareler → vision → betimleme."""
    import urllib.request
    import base64

    # 1. indir
    url = f"{bridge_base.rstrip('/')}/media/{media_id}"
    try:
        with tempfile.NamedTemporaryFile(suffix=".mp4", delete=False) as tmp:
            video_yol = Path(tmp.name)
        urllib.request.urlretrieve(url, video_yol)
    except Exception as e:
        return indirilemedi()

    import tempfile as _tf

    try:
        with _tf.TemporaryDirectory(prefix="video-kare-") as tmpdir:
            # Video karelerini çıkar
            try:
                kareler = onizleme.kare_cikar(video_yol, Path(tmpdir), adet=3)
            except onizleme.OnIslemeHatasi as e:
                return onisleme_hatasi(e)
            if not kareler:
                return onisleme_hatasi(Exception("kare çıkarılamadı"))

            # Her kare için vision
            try:
                sonuclar = _kareleri_betimle(kareler)
            except gorme.ModelYok:
                return vision_yok()
            except gorme.GormeHatasi as e:
                return onisleme_hatasi(e)

            # _ozetle GormeHatasi yutmaz (bkz. video_betimle notu).
            cevap = _ozetle(sonuclar)
            return (
                cevap.strip() if cevap else "Kareler alındı ama betimleme üretilmedi."
            )

    except Exception as e:
        return beklenmeyen(e)
    finally:
        try:
            video_yol.unlink()
        except Exception:
            pass


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("Kullanım: video.py <media_id|--dosya yol>", file=sys.stderr)
        sys.exit(1)

    arg = sys.argv[1]
    if arg == "--dosya":
        dosya = Path(sys.argv[2])
        print(video_betimle(dosya))
    else:
        bridge = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
        print(video_betimle_bridge(arg, bridge))
