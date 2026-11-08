"""gorme — ollama üzerinden vision modeline sor.

Neden ollama: modeller yerelde (RTX 3070 Ti, 8 GB), bulut çağrısı
gerekmez, görsel verisi dışarı çıkmaz. Kıyaslama
(tools/gorsel-test/karsilastir.py) 4 modeli aynı 3 görselde ölçtü.

Dayanıklılık — model yoksa hata değil, sıradakine geç:
    Görsel modelleri elle kurulur; biri silinmiş/indirilmemiş olabilir.
    Bu yüzden `modeller()` ollama'ya sorar ve istenen model listede
    yoksa sıradaki görsel modeline düşer. Hiçbiri yoksa OCR-only moda
    geçilir — kullanıcı yine bir şey alır, sessizce kaybolmaz.
"""

from __future__ import annotations

import base64
import json
import os
import urllib.error
import urllib.request
from pathlib import Path

API = os.getenv("OLLAMA_URL", "http://127.0.0.1:11434")
SORU_SURESI = int(os.getenv("GORSEL_SORU_SURESI", "240"))

# Tercih sırası — ÖLÇÜM SONUCU, tahmin değil.
# Kaynak: tools/gorsel-test/karsilastir.py, 3 görsel (Türkçe belge / şekil
#          tanıma / fatura), anahtar kelimeyle puanlama.
#
#   qwen2.5vl:3b   96,3p   26,9s   ← seçildi
#   qwen2.5vl:7b   96,3p   49,5s   aynı puan, 2 kat yavaş, 2 kat VRAM
#   tesseract      60,4p    0,3s   yalnız OCR; belge OKUR ama resim anlamaz
#   qwen3-vl:8b    33,3p  105,7s   düşünme jetonlarına takılıyor (bkz. not)
#   granite3.2-vision:2b 13,4p 25,3s  anlamsız cevap veriyor
#
# 3b, 7b ile aynı puanı yarı sürede verdiği için birincidir: WhatsApp'ta
# yanıt süresi kullanıcı deneyiminin kendisidir.
VARSAYILAN_MODELLER = [
    m.strip()
    for m in os.getenv(
        "GORSEL_MODELLER", "qwen2.5vl:3b,qwen2.5vl:7b,qwen3-vl:8b"
    ).split(",")
    if m.strip()
]

# qwen3-vl ve qwen3 ailesi DÜŞÜNEN modellerdir: görünür metinden önce
# uzun bir reasoning akışı üretirler. num_predict buna ayrılmazsa
# kullanıcı boş cevap alır. Kıyaslamada qwen3-vl:8b tam olarak bu
# yüzden iki belge testinde boş döndü (bkz. tools/gorsel-test/qwen3_yeniden.py).
DUSUNEN_MODELLER = ("qwen3", "deepseek-r1", "magistral", "phi4-reasoning")

# Düşünen modeller için daha geniş jeton penceresi.
DUSUNEN_JETON = int(os.getenv("GORSEL_DUSUNEN_JETON", "1600"))
NORMAL_JETON = int(os.getenv("GORSEL_MAX_TOKEN", "700"))


def dusunen_mi(model_adi: str) -> bool:
    return any(a in model_adi.lower() for a in DUSUNEN_MODELLER)


# Adı görsel yeteneği ima eden modeller. "3-vl" ya da ":vl" içermeyen
# bir model listede olsa bile buraya girmiyorsa görsel gönderilmez —
# metin-only model görseli alınca boş döner.
def gorsel_mi(model_adi: str) -> bool:
    kucuk = model_adi.lower()
    return any(
        i in kucuk
        for i in ("vl", "vision", "llava", "-v:", "bakllava", "minicpm", "moondream")
    )


# Belge metni elimizdeyse görsel modeline gerek yok: küçük bir metin-only
# model yeterli ve 8 GB VRAM'den çok daha az yer kaplar. Tercih sırası
# bilinçli olarak küçükten büyüğe.
VARSAYILAN_METIN_MODELLER = [
    m.strip()
    for m in os.getenv(
        "GORSEL_METIN_MODEL", "qwen3:4b,qwen3:8b,qwen2.5:7b,llama3.1:8b"
    ).split(",")
    if m.strip()
]


def metin_mi(model_adi: str) -> bool:
    """Görme/embedding/kodlama olmayan genel metin modeli mi?

    Embedding ve kodlama modelleri listede olabilir; belge özeti
    yapamazlar, yanlış cevap verirler.
    """
    kucuk = model_adi.lower()
    return not any(
        i in kucuk
        for i in ("embed", "minilm", "bge", "e5-", "coder", "vision", "-vl", "ocr")
    )


class GormeHatasi(RuntimeError):
    """Görme modeli cevap vermedi."""


class ModelYok(GormeHatasi):
    """Kullanılabilir görsel modeli yok — OCR-only moda düşülmeli."""


def modeller() -> list[str]:
    """ollama'da indirilmiş modellerin listesi."""
    try:
        with urllib.request.urlopen(f"{API}/api/tags", timeout=10) as c:
            veri = json.loads(c.read())
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError):
        return []

    adlar = []
    for m in veri.get("models", []):
        ad = (m.get("name") or "").strip()
        if ad and ad not in adlar:
            adlar.append(ad)
    return adlar


def gorsel_model_sec(tercih: list[str] | None = None) -> str:
    """Kullanılacak GÖRSEL modeli seçer.

    Tercih sırasına sadık kalır; ilk kurulu olan kazanır. Tercihte yoksa
    listedeki herhangi bir görsel modeline düşer (elle kurulmuş başka
    bir model varsa işe yaramaz olmaz). Hiç yoksa ModelYok.
    """
    kurulu = modeller()
    if not kurulu:
        raise ModelYok(f"ollama yanıt vermiyor ({API}) — servis kapalı olabilir")

    for ad in tercih or VARSAYILAN_MODELLER:
        if ad in kurulu:
            return ad
        # "qwen3-vl:8b" istenmiş ama "qwen3-vl:8b-instruct" kurulu olabilir
        for gercek in kurulu:
            if gercek.split(":")[0] == ad.split(":")[0] and gorsel_mi(gercek):
                return gercek

    gorseller = [m for m in kurulu if gorsel_mi(m)]
    if not gorseller:
        ilk = (tercih or VARSAYILAN_MODELLER or ["qwen3-vl:8b"])[0]
        raise ModelYok(
            f"ollama'da görsel modeli yok. Yüklemek için:  ollama pull {ilk}"
        )
    return gorseller[0]


def metin_model_sec(tercih: list[str] | None = None) -> str:
    """Belge metni için metin-only model seçer.

    Görsel model gerekmez: metin elimizde, sadece özet/yanıt üretilecek.
    Küçük model hem yeterli hem hızlı, 8 GB VRAM'de sorun çıkarmaz.

    ⛔ DÜŞÜNEN MODEL SEÇİLMEZ (varsa düşünmeyen bir model varsa).
    Bu kural ölçümden çıktı, tahminden değil (2026-10-06, video hattı).

    Ölçülen olay: `qwen3:4b` bir belge özetine çağrıldı. Görev "üç kare
    betimlemesini tek cümleye indir" — akıl yürütme değil, düzleştirme.
    Model 1600 jetonun tamamını düşünmeye harcadı, `done_reason: length`
    ile bitti, `response` alanı boş döndü. **224 saniye** sonra hata:

        GormeHatasi: model boş cevap döndü

    Ölçülen karşılaştırma (aynı istem, aynı makine):

        qwen3:4b      (düşünen)   211,1 sn   → BOŞ, hata
        qwen3:4b      think:false  88,5 sn   → İngilizce saçma, hata
        llama3.1:8b   (düşünmeyen) 19,4 sn   → doğru Türkçe cevap

    Yani düşünmeyi kapatmak ÇÖZÜM DEĞİL: model yine akıl yürütmesini
    `response` alanına yazıyor ve yine pencereyi dolduruyor. Çözüm, bu
    göreve düşünmeyen model vermek: **11 kat hızlı** ve çalışıyor.

    Bu bir hız tercihi değil, DOĞRULUK tercihi: düşünen model bu görevde
    ya boş döner ya da Türkçe olmayan bir saçma cevap verir.

    Tümü düşünen modellerse ilk düşünen modele düşülür — hiç model
    seçememekten iyidir.
    """
    kurulu = [m for m in modeller() if metin_mi(m)]
    if not kurulu:
        raise ModelYok(
            "ollama'da metin modeli yok (embedding/kodlama modelleri elendi)"
        )

    siralama = tercih or VARSAYILAN_METIN_MODELLER

    def tercih_sirasi(yalniz_dusunmeyen: bool) -> list[str]:
        """Tercih listesini sırayla gezip kurulu olan ilk eşleşmeyi verir."""
        for ad in siralama:
            if yalniz_dusunmeyen and dusunen_mi(ad):
                continue
            if ad in kurulu:
                return [ad]
            for gercek in kurulu:
                if gercek.split(":")[0] == ad.split(":")[0]:
                    return [gercek]
        return []

    # 1. geçiş: düşünmeyen model
    bulunan = tercih_sirasi(yalniz_dusunmeyen=True)
    if bulunan:
        return bulunan[0]

    # 2. geçiş: düşünen model (başka seçenek yok)
    bulunan = tercih_sirasi(yalniz_dusunmeyen=False)
    if bulunan:
        return bulunan[0]

    return kurulu[0]


def sor(
    model: str,
    yollar: list[Path],
    istem: str,
    zaman_asimi: int | None = None,
) -> str:
    """Görsel(ler)i modele verip cevap alır.

    Birden fazla yol verilirse (videodan kareler) hepsi tek istekte
    gönderilir: model kareleri sırayla görüp bütünü değerlendirir.
    """
    if not yollar:
        raise GormeHatasi("modele gösterilecek görsel yok")
    if not gorsel_mi(model):
        raise ModelYok(f"{model} görsel modeli değil (adında 'vl'/'vision' yok)")
    return _istek(model, yollar, istem, zaman_asimi)


def sor_menet(model: str, istem: str, zaman_asimi: int | None = None) -> str:
    """Görsel göndermeden soru — belge metnini özetletmek için."""
    return _istek(model, [], istem, zaman_asimi)


def _istek(
    model: str,
    yollar: list[Path],
    istem: str,
    zaman_asimi: int | None,
) -> str:
    """ollama /api/generate çağrısının ortak gövdesi."""
    zaman_asimi = zaman_asimi or SORU_SURESI

    govde = {
        "model": model,
        "prompt": istem,
        "images": [_b64(p) for p in yollar],
        "stream": False,
        # Modeli bellekte tut: ardışık mesajlarda her seferinde diskten
        # yüklemek ~30 sn ekliyor. 15m sonra kendiliğinden boşaltılır (GPU serbest kalır).
        # Kullanıcı kuralı (TODO.md): GPU modelleri 15 dakika boşta kalınca boşaltılmalıdır.
        "keep_alive": os.getenv("GORSEL_KEEP_ALIVE", "15m"),
        "options": {
            "temperature": 0,
            "top_p": 0.9,
            # Düşünen modeller jetonları önce akıl yürütmeğe harcar;
            # dar pencerede görünür metin hiç üretilmez.
            "num_predict": DUSUNEN_JETON if dusunen_mi(model) else NORMAL_JETON,
        },
    }

    istek = urllib.request.Request(
        f"{API}/api/generate",
        data=json.dumps(govde).encode(),
        headers={"Content-Type": "application/json"},
    )

    try:
        with urllib.request.urlopen(istek, timeout=zaman_asimi) as c:
            veri = json.loads(c.read())
    except urllib.error.HTTPError as err:
        govde_kisa = err.read()[:200].decode("utf-8", "replace")
        raise GormeHatasi(f"ollama HTTP {err.code}: {govde_kisa}") from err
    except (urllib.error.URLError, TimeoutError) as err:
        raise GormeHatasi(f"ollama'ya ulaşılamadı: {err}") from err

    cevap = (veri.get("response") or "").strip()
    if not cevap:
        raise GormeHatasi(_bos_cevap_tanis(veri))
    return cevap


def _bos_cevap_tanis(veri: dict) -> str:
    """Cevap boşsa NEDEN boş olduğunu söyler.

    ⛔ BOŞ SAYMAK YETMEZ. Ölçülen olay (2026-10-06, video hattı):
    `qwen3:4b` bir belge özetine 1600 jeton harcadı, tamamını
    düşünmeye kullandı, `done_reason: length` ile bitti ve `response`
    alanı BOŞ döndü. Eski hata metni "model boş cevap döndü" idi:
    ne oldu, neden oldu, ne yapılmalı — hiçbir şey söylemiyordu.
    224 saniye sonra gelen bu mesaj, olayın kendisi kadar belirsizdi.

    Üç ayrı boşluk var ve her biri FARKLI sebep:
      · done_reason=length + düşünme dolu → jeton penceresi bitti (asıl olay)
      · done_reason=length + düşünme boş → model uzun cevap üretmeyi kesti
      · done_reason=stop  + düşünme boş → model gerçekten bir şey söylemedi
    Bunları ayırmadan "boş cevap" demek, hatanın kendisi kadar
    teşhis edilemez bir hata üretir.
    """
    sebep = veri.get("done_reason") or "bilinmiyor"
    dusunme = (veri.get("thinking") or "").strip()

    if sebep == "length" and dusunme:
        return (
            f"model görünür metin üretmedi: {len(dusunme)} karakter düşünme "
            f"yazıp {veri.get('eval_count')} jetonluk pencereyi tüketti. "
            "Bu görev için düşünen (reasoning) model seçilmiş — özetleme/ "
            "betimleme akıl yürütme değil, düşünmeyen bir model gerekir"
        )
    if sebep == "length":
        return (
            f"model görünür metin üretmedi: {veri.get('eval_count')} jetonluk "
            "pencere bitti (GORSEL_MAX_TOKEN / GORSEL_DUSUNEN_JETON)"
        )
    return f"model boş cevap döndü (done_reason: {sebep})"


def _b64(yol: Path) -> str:
    return base64.b64encode(Path(yol).read_bytes()).decode()


def acik() -> bool:
    """ollama çalışıyor mu?"""
    return bool(modeller())


def saglik() -> str:
    """Tanıklama için tek satırlık durum: model sayısı, gpu yok, kapalı mı."""
    kurulu = modeller()
    if not kurulu:
        return "kapalı (ollama yanıt vermiyor)"
    gorsel = [m for m in kurulu if gorsel_mi(m)]
    return f"{len(kurulu)} model, {len(gorsel)} görsel: " + ", ".join(gorsel[:4])


def modelleri_bosalt(ollama_url: str = API) -> list[str]:
    """Ollama VRAM'inde yüklü olan modelleri hemen boşaltır (keep_alive: 0)."""
    try:
        istek = urllib.request.Request(f"{ollama_url}/api/ps")
        with urllib.request.urlopen(istek, timeout=5) as resp:
            veri = json.loads(resp.read())
        bosaltilan = []
        for m in veri.get("models", []):
            ad = m.get("name") or m.get("model")
            if ad:
                bosaltma_istegi = urllib.request.Request(
                    f"{ollama_url}/api/generate",
                    data=json.dumps({"model": ad, "keep_alive": 0}).encode(),
                    headers={"Content-Type": "application/json"},
                )
                try:
                    with urllib.request.urlopen(bosaltma_istegi, timeout=5):
                        bosaltilan.append(ad)
                except Exception:
                    pass
        return bosaltilan
    except Exception:
        return []
