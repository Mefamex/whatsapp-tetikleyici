"""istem — kullanıcının sorusunu bulur, model istemini kurar.

Kritik ayrım — mesaj metni ile soru aynı şey değil:
    Köprü `readableText()` (tetikleyici/message.go) bir görsel mesajı için
    "[image tipinde mesaj]" DÖNDÜRÜR — bu bir soru değil, bir yer tutucu.
    Onu modele soru diye verirsen model kullanıcının ne istediğini
    bilmeden uydurur.

    Gerçek soru üç yerden gelir:
      1. Görselin ALT YAZISI (WhatsApp'ta fotoğrafla birlikte yazılan
         metin). Köprüde message.Text olarak gelir.
      2. Görselden hemen ÖNCE yazılan metin ("bu ne?" + fotoğraf).
      3. HİÇBİRİ yoksa → serbest tarif iste.

Zemin olarak OCR metni eklenir: vision modeli küçük puntoyu uydurur,
tesseract onu doğru okur. Model hem görür hem doğru metne bakar.
"""

from __future__ import annotations

import os
import re

# Tetikleyicinin ürettiği yer tutucu: "[image tipinde mesaj]" vb.
YER_TUTUCU = re.compile(r"^\s*[\[(]\s*\w+\s+tipinde\s+mesaj\s*[\])]\s*$", re.IGNORECASE)

# Türkçe soru tespiti. SORU İŞARETİ YETERLİ DEĞİL: WhatsApp'ta insanlar
# "toplam ne", "fiyat kaç" yazıp işaret koymaz. Bu yüzden hem cümle
# BAŞI kalıpları hem cümle SONU edatları (ne/mı/mi/kaç) aranır.
SORU_ISARETI = re.compile(
    r"[?？]"
    r"|^(ne|nedir|nerede|neresi|kaç|kaçtır|hangi|haberi|neydi|neydi|"
    r"anlat|oku|okudun|açıkla|bak|çözdür|çöz|parse|oku musun|ne kadar|"
    r"toplam|toplamı|faturadaki|fiyatı)\b"
    # cümle sonu edatları: "toplam ne", "fiyat kaç", "doğru mu"
    r"|\b(ne|nedir|nerede|neresi|kaç|kaçtır|mı|mi|mu|mü|miydi)\s*[.!]?$",
    re.IGNORECASE,
)

EN_FAZLA_SORU = 300

SISTEM_TONU = (
    "WhatsApp'tan yazıyorsun. Karşındaki kişi Türkçe konuşuyor.\n"
    "KURALLAR:\n"
    "- Cevabın bir bilgi ya da cevap olsun. Görselden, resimden, "
    "metinden, dosyadan BAHSETME; konuşulan şeyin kendisini söyle.\n"
    "- Ne anlatman gerektiğinden emin değilsen onu yaz; uydurma.\n"
    "- Markdown, başlık, madde işareti, kalın yazım, numaralı liste ve "
    "emoji YOK.\n"
    "- En fazla 4-5 cümle. Tekrar, özet ve gereksiz açıklama yok."
)

# OCR metni güvenilmez: model bunu körü körüne kabul etmemeli.
#
# ÖLÇÜM (tools/prompt_varyant.py · 4 görsel × 3 tekrar): OCR metnini
# "aktarılacak metin" olarak veren istem 63,0p; "doğrulanacak zemin" olarak
# veren istem 75,0p aldı. Farkın kaynağı: "birebir aktar" talimatı modeli
# döküm yapmaya itiyordu, o da soruyu cevaplamak yerine metni tekrar ediyordu.
#
# ⚠️ "SENİN İÇİN ZEMİN; kullanıcıya aktarılacak metin DEĞİL" ifadesi ölçülmüş
# bir denge: hem aktarımı değil diyor hem de modele metni tamamen
# kullanılmaz gibi sunmuyor. 75,0p'lik istemin bu ifadeyi taşıyan hâli.
OCR_UYARI = (
    "Aşağıdaki metin bu görselden OCR ile okundu ve HATALI OLABİLİR. "
    "SENİN İÇİN ZEMİN; kullanıcıya aktarılacak metin DEĞİL. "
    "Görsele bakarak doğrula; çelişiyorsa görsele inan. "
    "Görselde olmayan bir şeyi uydurma."
)

# ⚠️ BURAYA SOMUT ÖRNEK KOYMA.
# Ölçüldü (tools/prompt_tune.py): isteme gerçek cevap örneği konunca
# qwen2.5vl:3b onu KOPYALIYOR. Şekiller görseli (hiç yazı yok) "Bu bir
# Monster dizüstü, 18.450 TL" cevabını verdi — çünkü örnekte o vardı.
# Kural biçimini açıkla, içeriğini gösterme.


def soru(
    tetik_mesaji: dict,
    gecmis: list[dict] | None = None,
    ortam: dict | None = None,
) -> tuple[str, str]:
    """Kullanıcının sorusunu ve kaynağını döndürür.

    Dönüş: (soru, kaynak) — kaynak: "baslik" | "onceki-mesaj" | "serbest"
    """
    ortam = os.environ if ortam is None else ortam

    # 1) Görselin alt yazısı
    baslik = _temiz((tetik_mesaji or {}).get("text"))
    if baslik and not YER_TUTUCU.match(baslik):
        return _kirp(baslik), "baslik"

    # 2) Görselden hemen önce yazılan, soru gibi görünen metin
    onceki = _onceki_soru(gecmis or [])
    if onceki:
        return _kirp(onceki), "onceki-mesaj"

    # 3) Ortam değişkenindeki TRIGGER_TEXT (yedek yol)
    trig = _temiz(ortam.get("TRIGGER_TEXT", ""))
    if trig and not YER_TUTUCU.match(trig):
        return _kirp(trig), "baslik"

    return "", "serbest"


def kur(
    tur: str,
    soru_metni: str,
    soru_kaynagi: str,
    ocr_metni: str = "",
    kare_sayisi: int = 1,
    belge_adi: str = "",
) -> str:
    """Vision modeline göndürilecek istemi kurar.

    OCR metni varsa modele hem "gör" hem "oku" yaptırılır; yoksa
    salt görsel okuma istenir.

    ⚠️ Buraya somut cevap örneği KOYMA. Ölçüldü
    (tools/prompt_tune.py): isteme örnek konunca qwen2.5vl:3b onu
    kopyalıyor — yazısız bir şekil görseline "Bu bir Monster dizüstü,
    18.450 TL" cevabı verdi, çünkü örnekte o vardı. Kuralın BİÇİMİNİ
    tarif et, içeriğini gösterme.
    """
    parcalar: list[str] = []

    # --- Ne yapılacak
    if tur == "document":
        taban = "Sana bir belge gönderdim. İçeriğini özetle."
    elif tur == "video":
        taban = (
            f"Bir videodan {max(1, kare_sayisi)} kare aldım, sırayla bunlar. "
            "Karelerde olan biteni anlat."
        )
    elif tur == "audio":
        taban = "Sana bir ses kaydı gönderdim."
    else:
        taban = "Sana bir görsel gönderdim."

    if soru_metni:
        kaynak_notu = {
            "baslik": "Kullanıcının yazdığı soru:",
            "onceki-mesaj": "Kullanıcı görselden hemen önce şunu sordu, ona cevap ver:",
        }.get(soru_kaynagi, "Kullanıcının sorusu:")
        parcalar.append(f'{taban}\n\n{kaynak_notu}\n"{soru_metni}"')
    else:
        parcalar.append(
            f"{taban}\n\nKullanıcı açıklama yazmamış, yalnız göndermiş. "
            "Ne olduğunu, içinde ne olduğunu kısaca söyle."
        )

    # --- OCR zemini
    # Ölçüm sonucu (tools/prompt_varyant.py, 4 görsel × 3 tekrar):
    # OCR metni "aktarılacak metin" olarak değil "doğrulanacak zemin"
    # olarak verilmeli. Aksi hâlde model okuduğunu tekrarlar, soruyu
    # cevaplamaz. Ölçülen fark: 63,0p → 75,0p.
    if ocr_metni:
        parcalar.append(f'{OCR_UYARI}\n\nOCR:\n"""\n{ocr_metni}\n"""')
        parcalar_extra = (
            "\n- Rakam, tarih, ad, kod gibi kritik bilgileri aynen aktar; "
            "kısma, yuvarlama, birleştirme yok."
        )
    else:
        parcalar_extra = "\n- Rakam, tarih, ad, kod gibi kritik bilgileri aynen aktar."

    # --- Uzunluk sınırı: WhatsApp'ta paragraf paragraf okunur
    parcalar.append(
        "\n\nKurallar:\n"
        "- Cevabın bir bilgi ya da cevap olsun; ortamı değil.\n"
        "- Görmediğin şeyi iddia etme; net değilse neyin belirsiz olduğunu yaz.\n"
        "- Markdown, başlık, madde işareti, kalın yazım, numaralı liste ve "
        "emoji YOK.\n"
        "- En fazla 4-5 cümle yaz." + parcalar_extra
    )

    return "\n".join(parcalar)


# ── video istem yardımcıları ───────────────────────────────────────────────────


def video_tek_kare(gorme_sonucu: str) -> str:
    """Tek karelik video için istemi kurar."""
    return (
        "Bir videodan tek kare aldım, aşağıdaki betimlemeyi verdim.\n"
        f"Kare betimlemesi:\n{gorme_sonucu}\n\n"
        "Bu kare videonun hangi anına denk gelir? Videoda ne oluyor, "
        "kimler var, ne oluyor kısaca anlat."
    )


def video_coklu_kare(gorme_sonuclari: list[str]) -> str:
    """Çoklu kareli video için istemi kurar."""
    if not gorme_sonuclari:
        return "Video kareleri alınamadı."

    parcalar = ["Bir videodan eşit aralıklı kareler aldım, sırayla betimlemeleri:"]
    for i, gorsel in enumerate(gorme_sonuclari, 1):
        parcalar.append(f"\n--- Kare {i} ---\n{gorsel}")

    parcalar.append(
        "\nBu kareler videonun kronolojik sırasındadır. "
        "Videoda ne oluyor, olayların sırası ne, ana olay ne? "
        "Kısaca özetle."
    )
    return "\n".join(parcalar)


# ── yardımcılar: metin temizliği ─────────────────────────────────────────────

_MEDYA_TURLERI = frozenset({"image", "video", "audio", "document", "sticker"})


def _temiz(metin) -> str:
    """Metni tek satıra indirger, kenar boşluklarını atar.

    Köprü metni olduğu gibi verir: satır sonu, sekme, kırılmaz boşluk ve
    bazen None gelir. Soru ararken bunları görmezden gelmek zorundayız.
    """
    if not isinstance(metin, str):
        return ""
    metin = metin.replace("\u00a0", " ")
    return re.sub(r"\s+", " ", metin).strip()


def _kirp(metin: str) -> str:
    """Soruyu makul uzunluğa indirger. Cümle sonunda kesmeyi dener."""
    if not isinstance(metin, str):
        return ""
    metin = _temiz(metin)
    if len(metin) <= EN_FAZLA_SORU:
        return metin
    return metin[:EN_FAZLA_SORU].rstrip() + "…"


def _onceki_soru(gecmis) -> str:
    """Geçmişten, tetikleyen mesajdan önceki son SORU gibi görünen metni bulur.

    Köprü geçmişi eskiden yeniye verir; tetikleyen mesaj en sonda olur, bu
    yüzden sondan geriye gidilir. Medya taşıyan mesajlar atlanır — bir sesli
    not, "toplam ne" sorusunun yerine geçmez.

    Soru olmayan bir metne gelindiğinde ARAMA DURUR: daha eskiye bakmak
    yanıltıcıdır, çünkü o metin bu görselle ilgili değildir.
    """
    if not isinstance(gecmis, (list, tuple)):
        return ""

    for m in reversed(gecmis):
        if not isinstance(m, dict):
            continue
        if str(m.get("type", "")).lower() in _MEDYA_TURLERI:
            continue
        metin = _temiz(m.get("text"))
        if not metin or YER_TUTUCU.match(metin):
            continue
        if SORU_ISARETI.search(metin):
            return metin
        return ""
    return ""


# ── cevap sadeleştirme ───────────────────────────────────────────────────────

# Vision modelleri isteme rağmen emoji üretir; WhatsApp'ta çirkin görünür.
_EMOJI = re.compile(
    "["
    "\U0001f000-\U0001ffff"
    "\U00002600-\U000027bf"
    "\U00002b00-\U00002bff"
    "\U0000fe00-\U0000fe0f"
    "\U0001f1e6-\U0001f1ff"
    "]",
    flags=re.UNICODE,
)


def yalın_isteme_gir(metin) -> str:
    """Model cevabını WhatsApp'ta görünecek düz metne çevirir.

    qwen2.5vl:3b isteme rağmen Markdown, emoji ve madde işareti üretir.

    ÖNEMLİ: yalnız SUNUM biçimini temizler, içeriğe dokunmaz. Modelin
    uydurduğu cevabı burada düzeltmek yanlış olur — ham cevap loglarda
    saklanır ve pipeline çıktısında aynen görünür.
    """
    if not isinstance(metin, str):
        return ""

    m = metin
    m = re.sub(r"```[^\n`]*\n?", "", m)  # kod bloğu açılışı
    m = m.replace("```", "")
    m = re.sub(r"\*\*(.+?)\*\*", r"\1", m)  # kalın
    m = re.sub(r"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)", r"\1", m)  # italik
    m = re.sub(r"^\s{0,3}#{1,6}\s*", "", m, flags=re.M)  # başlık
    m = re.sub(r"^\s*[-*•]\s+", "", m, flags=re.M)  # madde
    m = re.sub(r"^\s*\d+[.)]\s+", "", m, flags=re.M)  # numaralı liste
    m = _EMOJI.sub("", m)

    return "\n".join(s for s in (x.strip() for x in m.splitlines()) if s).strip()


# CEVAP_UZUNLUK_SINIRI, bir WhatsApp cevabının makul üst sınırı. Model
# uzun uzun açıklama yaparsa burada kesilir.
CEVAP_UZUNLUK_SINIRI = 1200


def tizle(metin, en_fazla: int = CEVAP_UZUNLUK_SINIRI) -> str:
    """Metni verilen uzunluğa indirger; kesildiyse "…" ile belli eder.

    Önce cümle sonunda kesmeyi dener (kesim anlamlı olsun), olmazsa kelime
    sınırında, en kötü hâlde karakter sınırında keser.
    """
    if not isinstance(metin, str):
        return ""

    m = metin.strip()
    if en_fazla <= 0 or len(m) <= en_fazla:
        return m

    dilim = m[:en_fazla]
    for ayirac in (". ", "! ", "? ", "\n"):
        konum = dilim.rfind(ayirac)
        if konum > en_fazla // 2:
            return dilim[: konum + len(ayirac)].rstrip() + "…"

    son_kelime = dilim.rfind(" ")
    if son_kelime > en_fazla // 2:
        dilim = dilim[:son_kelime]
    return dilim.rstrip() + "…"
