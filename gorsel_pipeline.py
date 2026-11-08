#!/usr/bin/env python3
"""gorsel_pipeline.py — WhatsApp görsel/belge/video → Türkçe cevap.

    tetikleyici (görsel geldi)
      → bu betik
        1. köprüden medyayı indir        (:mod:`gorsel.cek`)
        2. türünü anla, ön işleme yap    (:mod:`gorsel.onizleme`)
        3. metin katmanını çıkar         (:mod:`gorsel.ocr`, :mod:`gorsel.belge`)
        4. ollama modeline sor           (:mod:`gorsel.gorme`)
        5. istemi kur, cevabı sadeleştir (:mod:`gorsel.istem`)
      → cevap stdout'a düşer, tetikleyici sohbete yollar

Kullanım:
    gorsel_pipeline.py <media_id> [--output text|json] [--no-vision]
    gorsel_pipeline.py --dosya /path/to/image.jpg      (köprüsüz test)

Ortam değişkenleri (hepsi isteğe bağlı):
    BRIDGE_BASE        varsayılan http://127.0.0.1:31415
    OLLAMA_URL         varsayılan http://127.0.0.1:11434
    GORSEL_MODELLER    görsel model tercih sırası (virgüllü)
    GORSEL_METIN_MODEL yalnız metin sorusu için model (virgüllü)
    GORSEL_OCR_DIL     varsayılan tur+eng
    GORSEL_MAKS_KENAR  varsayılan 1600
    GORSEL_SORU_SURESI varsayılan 240 saniye
    GORSEL_KEEP_ALIVE  varsayılan 5m

ÇIKIŞ SÖZLEŞMESİ — bu betiğin en önemli kuralı:
    stdout DOLU olmak zorundadır. Tüm katmanlar başarısız olsa bile
    kullanıcıya Türkçe bir açıklama yazılır. Boş stdout, tetikleyicinin
    "çıktı boş" yoluna düşer; içerik kaybı olur. Sessizlikten kaçınmak
    bu projenin temel kuralıdır (docs/TESLIM-SORUNU-2026-10-05.md).
    Ayrıntılı tanıklama stderr'e gider.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import tempfile
import time
import traceback
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from gorsel import (  # noqa: E402
    belge,
    cek,
    deger_guvenligi,
    gorme,
    istem,
    ocr,
    onizleme,
    sticker,
)

BOS_MESAJ = "Görseli aldım ama içeriğini okuyamadım."


def log(mesaj: str) -> None:
    """İlerleme bilgisi stderr'e. stdout yalnız cevap için kalır."""
    print(f"[görsel] {mesaj}", file=sys.stderr, flush=True)


# ── Kullanıcıya giden hazır cevaplar ─────────────────────────────────────────
# Bunlar modelden büyük önemlidir: katman çökerse bile kullanıcı
# "ne oldu, neden anlamadın" demek yerine ne yapması gerektiğini görür.


def indirilemedi() -> str:
    return (
        "Görseli indiremedim, dosya bozuk geldi ya da köprüye ulaşılamadı. "
        "Bir daha göndermeyi dene."
    )


def vision_yok() -> str:
    return (
        "Görseli indirdim ama bu bilgisayarda görsel okuyan model yok, "
        "bu yüzden anlayamadım.\n\n"
        "Kurulum:  ollama pull "
        + (gorme.VARSAYILAN_MODELLER[0] if gorme.VARSAYILAN_MODELLER else "qwen3-vl:8b")
        + "\nSonra tekrar gönder."
    )


def onisleme_hatasi(hata: Exception) -> str:
    return f"Görseli indirdim ama işleyemedim ({hata}). Farklı bir dosya dene."


def beklenmeyen(hata: Exception) -> str:
    return (
        f"Görseli işlerken hata oldu ({type(hata).__name__}). "
        "Ayrıntı tetikleyici günlüğünde."
    )


# ── Ana akış ────────────────────────────────────────────────────────────────
def isle(
    medya_kimligi: str = "",
    kopru: str = cek.BRIDGE_BASE,
    vision_acik: bool = True,
    zorla_tur: str = "",
    yerel_yol: Path | None = None,
) -> dict:
    """Tüm katmanları çalıştırır, sonucu sözlük döndürür.

    Girdi ya mesaj KİMLİĞİDİR (köprüden indirilir) ya da yerel dosya
    yoludur (test). İkisi de aynı hatttan geçer — kopya yol yok, çünkü
    kopya yol zamanla gerçek yoldan ayrılır.

    Dönüş: cevap · tur · model · ocr · yontem · soru · soru_kaynagi ·
           sure · katmanlar · hata · tanik
    """
    baslangic = time.time()
    adimlar: list[str] = []
    tanik: dict = {}
    gecici = Path(tempfile.mkdtemp(prefix="gorsel-"))

    try:
        # ── 1. Medyayı ele geç ────────────────────────────────────────────
        if yerel_yol is not None:
            ham = gecici / ("ham" + (yerel_yol.suffix or ".bin"))
            shutil.copy2(yerel_yol, ham)
            mime = cek.tahmin(ham.read_bytes()[:64])
            adimlar.append("yerel")
            log(f"1/5 yerel dosya: {yerel_yol.name} · {mime or 'bilinmiyor'}")
        else:
            log(f"1/5 indiriliyor: {medya_kimligi}")
            ham, mime = cek.indir(medya_kimligi, gecici / "ham", kopru)
            adimlar.append(f"indir:{mime}")
            log(f"      {mime} · {ham.stat().st_size / 1024:.0f} KB")

        tur = zorla_tur or cek.mesaj_tipi(mime)
        tanik["mime"] = mime
        tanik["boyut"] = ham.stat().st_size

        # ── 2. Metin katmanı ───────────────────────────────────────────────
        # Belge metni OCR'dan önce gelir: PDF'te metin katmanı kesin
        # doğrudur, taranmış PDF'te zaten yoktur.
        metin, yontem = belge.metin_çikar(ham, mime)
        if metin:
            adimlar.append(f"belge:{yontem}")
            log(f"2/5 belge metni: {yontem} · {len(metin)} karakter")

        # ── 3. Görsel katmana hazırla ──────────────────────────────────────
        gorseller: list[Path] = []

        if tur == "image":
            hedef = gecici / "gorunum.jpg"
            onizleme.hazirla(ham, hedef)
            gorseller = [hedef]
            g, y = onizleme.olc(hedef)
            adimlar.append(f"onisleme:{g}x{y}")
            log(f"3/5 ön işleme: {g}x{y}")

        elif tur == "video":
            gorseller = onizleme.kare_cikar(ham, gecici, adet=3)
            if not gorseller:
                raise onizleme.OnIslemeHatasi("videodan kare çıkarılamadı")
            adimlar.append(f"kare:{len(gorseller)}")
            log(f"3/5 {len(gorseller)} kare çıkarıldı")

        elif tur == "document" and not metin:
            sayfa = onizleme.pdf_ilk_sayfa(ham, gecici / "sayfa")
            if sayfa is not None:
                hedef = gecici / "sayfa.jpg"
                try:
                    onizleme.hazirla(sayfa, hedef)
                    gorseller = [hedef]
                    adimlar.append("pdf->gorsel")
                except onizleme.OnIslemeHatasi:
                    gorseller = [sayfa]
                    adimlar.append("pdf->gorsel:ham")
                log(f"3/5 PDF metin katmanı yok → sayfa görsele çevrildi")
            else:
                adimlar.append("pdf:cozulmedi")
                log("3/5 PDF sayfası görsele çevrilemedi")

        elif tur == "sticker":
            # Sticker: webp -> png -> OCR -> Vision (gorsel/sticker.py).
            # Ayrı dal: document zincirine girmez, kendi sonucu döner.
            sticker_yol = gecici / "sticker.webp"
            shutil.copy2(ham, sticker_yol)
            adimlar.append("sticker:webp")
            try:
                sticker_metin = sticker.sticker_isle(sticker_yol)
                return _sonuc(
                    sticker_metin,
                    adimlar + ["sticker:islenmiş"],
                    sure=time.time() - baslangic,
                    tanik=tanik,
                )
            except Exception as e:
                log(f"Sticker işleme hatası: {e}")
                return _sonuc(
                    onisleme_hatasi(e),
                    adimlar + ["sticker:hata"],
                    hata=str(e),
                    sure=time.time() - baslangic,
                    tanik=tanik,
                )

        # ── 4. OCR ─────────────────────────────────────────────────────────
        # Görsel katman varsa ve henüz metin yoksa. OCR tek başına
        # (vision yoksa) hem de modele zemin olarak işe yarar.
        #
        # ⛔ VİDEODA OCR ÇALIŞTIRILMAZ. Ölçüm ve iki aday çözümün karşılaştırması
        # `_ocr_gerekli` docstring'inde. Kısaca: video yolunda OCR modelin
        # cevabını "yazıyı aktar" moduna kaydırıyordu; üç sahneli videoda
        # cevap "Kare 1: Sahne 1, 6." idi, yani üç kareden biri. OCR'siz
        # aynı video üç sahneyi de doğru renkleriyle anlatıyor.
        if gorseller and not metin:
            if not _ocr_gerekli(tur):
                log("4/5 OCR atlandı (video: vision modeli metni kendisi okur)")
                adimlar.append("ocr:video-atlandi")
            elif ocr.var_mi():
                log("4/5 OCR (tesseract)")
                try:
                    metin, psm = ocr.calistir(gorseller[0])
                    if metin:
                        adimlar.append(f"ocr:psm{psm}")
                        log(f"      {len(metin)} karakter (psm {psm})")
                    else:
                        adimlar.append("ocr:bos")
                except ocr.OcrHatasi as e:
                    adimlar.append("ocr:hata")
                    log(f"      OCR atlandı: {e}")
            else:
                adimlar.append("ocr:yok")
                log("4/5 tesseract yok, OCR atlandı")
        elif not gorseller and not metin:
            adimlar.append("metin:katman-yok")
            log("4/5 ne metin ne görsel katmanı var")

        # ── 5. Soruyu bul ve modele sor ─────────────────────────────────────
        ortam = os.environ
        tetik = cek.tetikleyici_json(ortam)
        gecmis = tetik.get("history") or []
        soru_metni, kaynak = istem.soru(tetik, gecmis, ortam)
        tanik["soru_kaynagi"] = kaynak
        log(
            f"5/5 soru kaynağı: {kaynak}"
            + (f" → {soru_metni[:60]}" if soru_metni else " (serbest)")
        )

        model_adi, cevap, hata = modele_sor(
            gorseller, tur, metin, soru_metni, kaynak, vision_acik
        )
        tanik["model"] = model_adi
        adimlar.append("model:" + (model_adi or "yok"))

        # ── Kurtarma: model çalışmadıysa elimizdekini kullan ────────────────
        if not cevap:
            adimlar.append("kurtarma")
            if metin:
                cevap = (
                    "Görseli okudum ama yorumlayacak model yanıt vermedi. "
                    "Görselde yazanlar şunlar:\n\n" + ocr.ozet(metin)
                )
                adimlar[-1] = "kurtarma:ocr-meti"
            elif isinstance(hata, gorme.ModelYok):
                cevap = vision_yok()
            elif hata is not None:
                cevap = onisleme_hatasi(hata)
            else:
                cevap = BOS_MESAJ

        # ── Kurtarma 2: cevap VAR ama içeriği taşımıyor ──────────────────────
        # Ölçülen ayrı bir arıza (tools/prompt_varyant.py, d-kucuk.png):
        # model yanıt veriyor ama alan adlarını sayıp DEĞERLERİ atıyor —
        #     «… MARUZ ATILMA KODU, SERİ NO, ODENEN tutar, IBAN… bilgileri var.»
        # Kullanıcı "toplam ne" sorduğunda alacağı cevap budur: işe yaramaz.
        # Boş cevap yakalayan yukarıdaki dal bunu GÖRMEZ; ayrı bir durum.
        #
        # Somut soru varsa model doğru çalışıyor (ölçüldü: «Toplam 4.960,50
        # TL.»), yani kural yalnız serbest anlatımda devreye giriyor.
        if cevap and metin:
            kayip, gerekce = deger_guvenligi.kayip_mi(cevap, metin)
            if kayip:
                log(f"⚠ değer kaybı: {gerekce}")
                cevap = (
                    "Okudum ama özetleyemedim, ne yazdığını olduğu gibi "
                    "aktarıyorum:\n\n" + ocr.ozet(metin)
                )
                adimlar.append("kurtarma:deger-kaybi")
                tanik["deger_kaybi"] = gerekce

        adimlar.append("cevap:var" if cevap else "cevap:yok")

        return {
            "cevap": istem.tizle(istem.yalın_isteme_gir(cevap)),
            "tur": tur,
            "model": model_adi,
            "ocr": ocr.ozet(metin) if metin else "",
            "yontem": yontem,
            "soru": soru_metni,
            "soru_kaynagi": kaynak,
            "sure": round(time.time() - baslangic, 1),
            "katmanlar": adimlar,
            "hata": str(hata) if hata else "",
            "tanik": tanik,
        }

    except cek.IndirmeHatasi as e:
        log(f"İNDİRME HATASI: {e}")
        return _sonuc(
            indirilemedi(),
            adimlar=["indir:hata"],
            hata=str(e),
            sure=time.time() - baslangic,
            tanik=tanik,
        )
    except onizleme.OnIslemeHatasi as e:
        log(f"ÖN İŞLEME HATASI: {e}")
        return _sonuc(
            onisleme_hatasi(e),
            adimlar=["onisleme:hata"],
            hata=str(e),
            sure=time.time() - baslangic,
            tanik=tanik,
        )
    except Exception as e:  # noqa: BLE001 — kullanıcıya tek satır gitsin
        log("BEKLENMEYEN HATA:\n" + traceback.format_exc())
        return _sonuc(
            beklenmeyen(e),
            adimlar=["beklenmeyen:hata"],
            hata=str(e),
            sure=time.time() - baslangic,
            tanik=tanik,
        )
    finally:
        shutil.rmtree(gecici, ignore_errors=True)


def _sonuc(
    cevap: str,
    adimlar: list[str],
    hata: str = "",
    sure: float = 0.0,
    tanik: dict | None = None,
) -> dict:
    """Tek noktadan dönüş sözlüğü — anahtar unutmamak için."""
    return {
        "cevap": cevap,
        "tur": "",
        "model": "",
        "ocr": "",
        "yontem": "",
        "soru": "",
        "soru_kaynagi": "",
        "sure": round(sure, 1),
        "katmanlar": adimlar,
        "hata": hata,
        "tanik": tanik or {},
    }


# ── Model katmanı ───────────────────────────────────────────────────────────
def _ocr_gerekli(tur: str) -> bool:
    """Bu tür için OCR katmanı çalıştırılmalı mı?

    ⛔ VİDEODA OCR ÇALIŞTIRILMAZ — bu bir ön karar, ölçümle gerekçelendirildi.

    Belki beklenmedik: burada bir hata düzeltilirken iki aday çözüm ölçüldü ve
    KAYBEDEN biri seçildi.

    Ölçüm (üç sahneli sentetik video: kırmızı "SAHNE 1", yeşil "SAHNE 3",
    mavi "SAHNE 5"; üç kare çıkarıldı, model `qwen2.5vl:3b`):

    | kare OCR'ı | cevap | sonuç |
    |---|---|---|
    | hiç | "Kare 1: Sahne 1, **kırmızı arka plan**, beyaz metinle "SAHNE 1"… / Kare 2: …**yeşil arka plan**… / Kare 3: …**mavi arka plan**…" | ✅ üç sahne, renkler doğru, yazılar da aktarıldı |
    | tüm kareler, etiketli | "1. Sahne 1: 6 / 2. Sahne 3: 6 / 3. Sahne 5: 6" | ⚠️ üç kareyi de kapsıyor ama betimleme yok, yalnız OCR tekrarı |
    | tüm kareler + "önce anlat, sonra yazıları aktar" | "…bir karakterin yüzü ve elini gösteren kare…" | ❌ **HALÜSİNASYON**: renkli düz karelerde ne yüz ne el var |
    | yalnız 1. kare (üretimdeki hâli) | "Kare 1: Sahne 1, 6." | ❌ üç kareden biri |

    İki bulgu:

    1. **OCR'siz en iyi sonuç.** Vision modeli zaten metni okuyor; OCR
       katmanı bilgi EKLEMİYOR, yalnız modelin "yazıyı aktar" moduna
       kaydırıyor. Kaybedilen bir şey yok — OCR'siz cevapta yazılar da
       aktarıldı ("beyaz metinle "SAHNE 1" yazısı").

    2. **"Önce anlat, sonra aktar" talimatı modeli UYDURMAYA itiyor.**
       Sezgisel olarak doğru görünen ve burada da işe yaramayan çözüm.
       Modele iki iş verilince "betimleme" görevini görselden değil
       metinden dolduruyor: "sahne" sözcüğü OCR'dan geldiği için "bir
       karakterin yüzü" diye uyduruyor. Betimleme hiç istenmediğinde
       model yalnız görebildiğini söylüyor.

    `GORSEL_VIDEO_OCR=1` bu kararı geri alır: ölçüm tek bir model, tek bir
    video ve tek bir istem üzerinden alındı; sahibin farklı bir önceliği
    olabilir ve bu değişken o önceliğe açık kapı bırakır.

    Göz karşı olsun diye: OCR tamamen kaldırılmıyor. Fotoğraf, belge,
    sticker ve ekran görüntüsü yolları değişmedi — yalnız video.
    """
    if tur != "video":
        return True
    return os.getenv("GORSEL_VIDEO_OCR") == "1"


def modele_sor(
    gorseller: list[Path],
    tur: str,
    metin: str,
    soru_metni: str,
    kaynak: str,
    vision_acik: bool,
) -> tuple[str, str, Exception | None]:
    """Doğru modele sorar. Dönüş: (model, cevap, hata).

    Üç yol:
      • vision kapalı          → ham metni döndür (OCR-only)
      • görsel katman var      → vision modeli (gör + oku)
      • yalnız metin katmanı  → metin-only model ( belge özeti )
    """
    if not vision_acik:
        log("      vision kapalı (--no-vision): ham metin dönecek")
        return "", metin_cevabi(metin, gorsel_var=bool(gorseller)), None

    if gorseller:
        try:
            model = gorme.gorsel_model_sec()
        except gorme.ModelYok as e:
            return "", "", e
        except Exception as e:  # noqa: BLE001
            return "", "", e

        log(f"      model: {model}")
        istem_metni = (
            istem.SISTEM_TONU
            + "\n\n"
            + istem.kur(
                tur=tur,
                soru_metni=soru_metni,
                soru_kaynagi=kaynak,
                ocr_metni=ocr.ozet(metin) if metin else "",
                kare_sayisi=len(gorseller),
            )
        )
        try:
            return model, gorme.sor(model, gorseller, istem_metni), None
        except gorme.GormeHatasi as e:
            log(f"      vision hatası: {e}")
            return model, "", e

    if metin:
        try:
            model = gorme.metin_model_sec()
        except gorme.ModelYok as e:
            return "", "", e
        except Exception as e:  # noqa: BLE001
            return "", "", e

        log(f"      metin-only model: {model}")
        soru = soru_metni or "Bu belgede ne var, özetle."
        istem_metni = istem.SISTEM_TONU + (
            "\n\nSana bir belgenin metnini veriyorum.\n\n"
            f'Belge metni:\n"""\n{ocr.ozet(metin)}\n"""\n\n'
            f'Kullanıcının isteği: "{soru}"\n\n'
            "Belge metnini esas al, dışarıdan bilgi katma."
        )
        try:
            return model, gorme.sor_menet(model, istem_metni), None
        except gorme.GormeHatasi as e:
            log(f"      metin modeli hatası: {e}")
            return model, "", e

    return "", "", None  # ne görsel ne metin: çağıran kurtarma yolunu kullanır


def metin_cevabi(metin: str, gorsel_var: bool = True) -> str:
    """Vision kapalıyken OCR metnini olduğu gibi döndürür.

    "Yazı yok" ile "okuyamadım" ayrı mesajdır: fotoğrafta yazı olmaması
    normaldir, kullanıcı bunu modelin arızası sanmamalı.
    """
    if metin:
        return "Görselde okunanlar:\n\n" + ocr.ozet(metin)
    if not gorsel_var:
        return (
            "Medyada okunabilir yazı bulamadım. Bu bir fotoğrafsa anlamını "
            "çıkarmak için görsel modeli gerekiyor; şu an kapalı."
        )
    return "Bu görselde okunabilir yazı yok. Fotoğrafın ne olduğunu tarif etmem için görsel modeli gerekiyor."


# ── Giriş noktası ───────────────────────────────────────────────────────────
def main() -> int:
    ap = argparse.ArgumentParser(
        description="WhatsApp görseli/belgesi indirir, okur, Türkçe cevap verir.",
    )
    ap.add_argument("media_id", nargs="?", help="mesajdaki medya kimliği")
    ap.add_argument("--dosya", help="köprü yerine yerel dosya (test için)")
    ap.add_argument("--bridge", default=cek.BRIDGE_BASE, help="köprü taban adresi")
    ap.add_argument("--output", choices=("text", "json"), default="text")
    ap.add_argument(
        "--no-vision", action="store_true", help="yalnız OCR, model çağırma"
    )
    ap.add_argument("--tur", default="", help="türü zorla: image|video|document")
    args = ap.parse_args()

    if not args.media_id and not args.dosya:
        ap.error("media_id ya da --dosya gerekli")

    veri = isle(
        medya_kimligi=args.media_id or "",
        kopru=args.bridge,
        vision_acik=not args.no_vision,
        zorla_tur=args.tur,
        yerel_yol=Path(args.dosya).expanduser() if args.dosya else None,
    )

    if args.output == "json":
        print(json.dumps(veri, ensure_ascii=False, indent=2))
    else:
        print(veri.get("cevap") or BOS_MESAJ)

    # Çıkış kodu 0: betik kendi hatasını kullanıcıya ZATEN söyledi.
    # 1 dönmek, tetikleyicinin de "hata" demesine yol açardı ve aynı
    # mesaj iki kez giderdi.
    return 0


if __name__ == "__main__":
    sys.exit(main())
