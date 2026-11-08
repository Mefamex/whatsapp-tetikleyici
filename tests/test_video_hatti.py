#!/usr/bin/env python3
"""Video hattı + gorme model seçimi regresyon testleri.

Çalıştırma:
    python3 -m unittest discover -s tests -p 'test_video*' -v

Bu dosyadaki her test bir ÖLÇÜLMÜŞ hataya karşılık gelir. Yorumlarda
yazan süreler ve hata metinleri tahmin değil, o gün alınan çıktıdır.

┌──────────────────────────────────────────────────────────────────┐
│ ÖLÇÜLEN OLAY (2026-10-06)                                        │
│                                                                  │
│ Video hattı ilk kez uçtan uca çalıştırıldı ve ÇALIŞMADI.         │
│   · kare çıkarma      → 3 kare, 0,2 sn        ✓                  │
│   · kare betimleme    → 32,0 sn               ✓                  │
│   · özetleme          → 211,1 sn → BOŞ CEVAP ✗                  │
│                                                                  │
│ Kullanıcıya giden: "Videodan kareler aldım ama işleyemedim"      │
│ Yani 3 kare betimlemesi elimizdeyken kullanıcıya HİÇBİR ŞEY       │
│ verilmedi, üstelik 224 saniye beklettikten sonra.                 │
│                                                                  │
│ Sebep: `metin_model_sec()` düşünen (reasoning) bir model seçmişti  │
│ (`qwen3:4b`). Özetleme akıl yürütme değil; model 1600 jetonun      │
│ tamamını düşünmeye harcadı ve `response` alanı boş döndü.          │
│                                                                  │
│ Karşılaştırma ölçümü (aynı istem, aynı makine):                   │
│   qwen3:4b      düşünen     211,1 sn → BOŞ, HATA                  │
│   qwen3:4b      think:false  88,5 sn → İngilizce saçma, HATA      │
│   qwen2.5:7b    düşünmeyen    ?     → doğru cevap                 │
│   llama3.1:8b   düşünmeyen   19,4 sn → doğru cevap                │
└──────────────────────────────────────────────────────────────────┘
"""

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

KOK = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(KOK))

from gorsel import gorme, video  # noqa: E402

# Üretim yolu. `gorsel_pipeline` modül düzeyinde yan etki yapabilir (env
# okuması), bu yüzden burada import ediliyor — testler bu yolun davranışını
# sınıyor, klonun değil.
import gorsel_pipeline as pipeline  # noqa: E402


# ── metin_model_sec: düşünen model seçilmemeli ────────────────────────────


class TestMetinModelSecim(unittest.TestCase):
    """metin_model_sec'in en canlı hatayı önlemesi.

    Bu test olmadan kod "çalışıyor" görünür ve video hattı her çağrıda
    224 saniye sonra boş hata verir.
    """

    def test_dusunen_model_tercih_sirasinda_ysa_dusunmeyene_gecer(self):
        """qwen3:4b (düşünen) qwen2.5:7b'den ÖNCE geliyor, yine geçilmeli.

        Ölçülen olayın tam kendisi: varsayılan tercih sırasının ilk elemanı
        `qwen3:4b` ve o kuruluydu.
        """
        kurulu = ["qwen3:4b", "qwen2.5:7b", "qwen3-vl:8b"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            secilen = gorme.metin_model_sec()

        self.assertEqual(
            secilen,
            "qwen2.5:7b",
            f"düşünmeyen model seçilmeliydi, seçilen: {secilen}",
        )
        self.assertFalse(
            gorme.dusunen_mi(secilen),
            "özetleme gibi akıl yürütmeyen bir göreve düşünen model verilmemeli",
        )

    def test_dusunen_model_ad_tam_eslesmede_gecer(self):
        """Etiket tam eşleşmesi varsa sıra korunur — düşünmeyen olan kazanır."""
        kurulu = ["qwen3:4b", "qwen2.5:7b"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            self.assertEqual(gorme.metin_model_sec(), "qwen2.5:7b")

    def test_hepsi_dusunen_model_dusuneni_dondurur(self):
        """Hiç düşünmeyen model yoksa HATA VERMEZ, düşüneni döndürür.

        Model seçememek, çalışan bir modelle çalışmaktan kötüdür: kullanıcı
        hiç cevap almaz. Bu yüzden ikinci geçiş var.
        """
        kurulu = ["qwen3:4b", "qwen3:8b"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            secilen = gorme.metin_model_sec()
        self.assertTrue(gorme.dusunen_mi(secilen))
        self.assertEqual(secilen, "qwen3:4b")

    def test_etiket_eslesmesi_calisir(self):
        """Tercih listesindeki etiket kurulu değilse aynı taban ad aranır.

        Bu özellik düşünmeyen/geçen ayrımından ÖNCE de vardı; ayrım eklenirken
        bozulmadığı buradan doğrulanır.
        """
        kurulu = ["qwen2.5:7b-instruct", "qwen3:4b"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            self.assertEqual(gorme.metin_model_sec(), "qwen2.5:7b-instruct")

    def test_gorsel_modeller_metin_candidate_olmaz(self):
        """Görsel model listede olsa bile metin adayı seçilmez.

        `metin_mi` elemesi bu testin konusu değil ama bir regresyon olurdu:
        düşünmeyen model ararken görsel model seçilirse, metin-only bir
        soruya görsel model cevap vermeye çalışır.
        """
        kurulu = ["qwen3-vl:8b", "qwen2.5vl:3b", "llama3.1:8b"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            secilen = gorme.metin_model_sec()
        self.assertFalse(gorme.gorsel_mi(secilen), f"görsel model seçildi: {secilen}")

    def test_model_hicbiri_kurulu_degilse_hata_verir(self):
        """Hiç metin modeli yoksa anlaşılır hata verilir (sessiz geçilmez)."""
        kurulu = ["qwen3-vl:8b", "nomic-embed-text"]
        with mock.patch.object(gorme, "modeller", return_value=kurulu):
            with self.assertRaises(gorme.ModelYok) as cm:
                gorme.metin_model_sec()
        self.assertIn("metin modeli yok", str(cm.exception))


# ── boş cevap teşhisi ───────────────────────────────────────────────────────


class TestBosCevapTaniSine(unittest.TestCase):
    """`_bos_cevap_tanis`: "boş" demek yetmez, SEBEBİ söylenmeli.

    Eski hata metni: "model boş cevap döndü". 224 saniye sonra gelen bu
    mesaj, olayın kendisi kadar belirsizdi. Üç ayrı boşluk var ve üçü de
    farklı sebep.
    """

    def test_jeton_penceresi_dusunmeye_tukendi(self):
        """⛔ ASIL OLAY: done_reason=length + dolu düşünme."""
        veri = {
            "done_reason": "length",
            "thinking": "Okay, I need to summarize the given Turkish sentence...",
            "eval_count": 1600,
            "response": "",
        }
        mesaj = gorme._bos_cevap_tanis(veri)

        self.assertIn("düşünme", mesaj)
        self.assertIn("1600", mesaj)
        self.assertIn("reasoning", mesaj, "hangi model seçildiğini söylemeli")

    def test_jeton_bitti_dusunme_yok(self):
        """length + düşünme boş: sebep yine pencerenin bitmesi, farklı not."""
        veri = {"done_reason": "length", "thinking": "", "eval_count": 700}
        mesaj = gorme._bos_cevap_tanis(veri)

        self.assertIn("700", mesaj)
        self.assertIn("MAX_TOKEN", mesaj, "ayarlanacak değişken adını vermeli")

    def test_model_normal_durustu_bos_sonuc(self):
        """done_reason=stop: model gerçekten bir şey söylemedi.

        ⛔ Bu dal "jeton bitti" DEMEZ. Model penceresini doldurmadan durdu;
        kullanıcıya "GORSEL_MAX_TOKEN yükseltin" demek yanlış yön gösterir.
        """
        veri = {"done_reason": "stop", "thinking": "", "eval_count": 12}
        mesaj = gorme._bos_cevap_tanis(veri)

        self.assertIn("stop", mesaj)
        self.assertNotIn(
            "MAX_TOKEN", mesaj, "stop durumunda pencere bitmemiş — yol gösterme"
        )
        self.assertNotIn("düşünme", mesaj, "düşünme boştu — düşünmeyi suçlamak yanlış")

    def test_done_reason_yoksa_bilinmiyor_denir(self):
        """Eksik yanıtta uydurma sebep üretilmez."""
        mesaj = gorme._bos_cevap_tanis({})
        self.assertIn("bilinmiyor", mesaj)


# ── özetleme başarısız olursa sonuç kaybolmamalı ───────────────────────────


class TestOzetlemeKacisi(unittest.TestCase):
    """⛔ EN PAHALI HATA: 3 kare betimlemesi varken kullanıcıya hiçbir şey gitmiyordu.

    Özetleme bir SÜS adımı. Başarısız olursa eldeki kare betimlemeleri
    kullanıcıya verilmelidir.
    """

    KARELER = [
        "Bir oda, ortada masa, üstünde kâğıtlar var.",
        "Aynı oda, açık bir kapı ve pencere görünüyor.",
        "Masanın üstünde kahve ve dizüstü bilgisayar var.",
    ]

    def test_ozet_modeli_bos_donse_kareler_kalir(self):
        """Özetleme modeli boş dönerse kare betimlemeleri döner."""
        with (
            mock.patch.object(gorme, "metin_model_sec", return_value="test-model"),
            mock.patch.object(
                gorme,
                "sor_menet",
                side_effect=gorme.GormeHatasi("model boş cevap döndü"),
            ),
        ):
            sonuc = video._ozetle(self.KARELER)

        self.assertIn("1. Bir oda", sonuc)
        self.assertIn("2. Aynı oda", sonuc)
        self.assertIn("3. Masanın", sonuc)

    def test_ozet_modeli_yoksa_kareler_kalir(self):
        """Metin modeli hiç yoksa da kareler kaybolmaz."""
        with mock.patch.object(
            gorme, "metin_model_sec", side_effect=gorme.ModelYok("yok")
        ):
            sonuc = video._ozetle(self.KARELER)

        self.assertIn("1. Bir oda", sonuc)

    def test_kacis_yolu_kullaniciya_durustluk_soyler(self):
        """⛔ Kullanıcıya eksik olduğu SÖYLENMELİ.

        Sessizce daha kötü bir cevap vermek, tam vermemekten iyi değildir.
        """
        with (
            mock.patch.object(gorme, "metin_model_sec", return_value="test-model"),
            mock.patch.object(gorme, "sor_menet", side_effect=gorme.GormeHatasi("boş")),
        ):
            sonuc = video._ozetle(self.KARELER)

        self.assertIn("tek cümleye indiremedim", sonuc)

    def test_ozet_bos_string_donse_kareler_kalir(self):
        """Model hata bile vermeyip boş metin dönerse de sonuç kaybolmaz."""
        with (
            mock.patch.object(gorme, "metin_model_sec", return_value="test-model"),
            mock.patch.object(gorme, "sor_menet", return_value="   \n  "),
        ):
            sonuc = video._ozetle(self.KARELER)

        self.assertIn("1. Bir oda", sonuc)

    def test_normal_yolda_ozet_donder(self):
        """Kacis yolu normal çalışmayı bozmamalı.

        `metin_model_sec` de taklit edilir: eskiden gerçek ollama'da metin
        modeli olmasına bağlıydı ve ollama kapalıyken (2026-10-14) alakasız
        şekilde düşüyordu. Test artık ortama bağlı değil.
        """
        with (
            mock.patch.object(gorme, "metin_model_sec", return_value="test-model"),
            mock.patch.object(gorme, "sor_menet", return_value="Sade bir ofis."),
        ):
            sonuc = video._ozetle(self.KARELER)

        self.assertEqual(sonuc, "Sade bir ofis.")

    def test_tek_kare_kacisinda_siralama_bozulmaz(self):
        """Tek kare betimlemesi de kacış yolunda korunur."""
        with (
            mock.patch.object(gorme, "metin_model_sec", return_value="test-model"),
            mock.patch.object(gorme, "sor_menet", side_effect=gorme.GormeHatasi("boş")),
        ):
            sonuc = video._ozetle(["Tek bir kare betimlemesi."])

        self.assertIn("1. Tek bir kare betimlemesi.", sonuc)


# ── çelişen/ölü sabitler ───────────────────────────────────────────────────


class TestSureSabitleri(unittest.TestCase):
    """`GORSEL_KARE_SURESI` iki ilgisiz şeyi kontrol ediyordu.

    onizleme.py'de ffmpeg ALT SÜREÇ zaman aşımıydı (60 sn), video.py'de
    vision soru süresiydi (300 sn). Aynı ada iki varsayılan. Kullanıcı
    vision'a süre vermek için yükseltirse ffmpeg'e de aynı süre gidiyor;
    tersi de doğru — ffmpeg'i hızlandırmak için 30 sn yazarsa vision çağrısı
    da 30 sn'ye düşer ve ölçülen 32 sn'lik kare her seferinde zaman aşımına
    uğrar.
    """

    def test_ffmpeg_suresi_yeni_adini_kullanir(self):
        self.assertEqual(onizleme_suresi(), "60")

    def test_video_kare_cagri_suresi_olculen_sureyi_asar(self):
        """⛔ Varsayılan 30 sn OLAMAZ: ölçülen tek kare süresi 32,0 sn.

        30 sn'lik bir bütçe modeli her karede zaman aşımına uğratırdı.
        """
        self.assertGreaterEqual(
            video.KARE_CAGRI_SURESI,
            32,
            "ölçülen 32 sn'lik kare için bütçe çok dar",
        )

    def test_ollama_kilitlenme_tavaninin_altinda(self):
        """300 sn'yi aşan ollama çağrıları servisi kilitliyor.

        Tavan 300'ün altında olmalı: hata dönecekse kullanıcı 2 dakika
        bekleyip yine hata almasın.
        """
        self.assertLess(
            video.KARE_CAGRI_SURESI,
            300,
            "300 sn üstü ollama çağrıları servisi kilitliyor",
        )

    def test_olu_sabit_kalmadi(self):
        """video.py'deki ölü sabitler geri gelmemeli."""
        for ad in ("KARE_SURESI", "MAKS_KARE", "MAKS_KENAR"):
            self.assertFalse(
                hasattr(video, ad),
                f"video.{ad} ölüydü; kullanılmıyorsa tanımlanmamalı",
            )

    def test_eski_ortak_ad_iki_modulde_kullanilmiyor(self):
        """Aynı değişken adı iki modülde de kullanılmamalı."""
        import gorsel.onizleme as on

        self.assertEqual(on.FFMPEG_SURESI, 60, "ffmpeg alt süreç bütçesi 60 sn olmalı")
        self.assertFalse(
            hasattr(on, "KARE_SURESI"),
            "onizleme.KARE_SURESI çakışmayı sürdürür — silinmeli",
        )


def onizleme_suresi() -> str:
    """onizleme'nin ffmpeg bütçesini döner (sabit olarak yazıldı ki test okusun)."""
    import gorsel.onizleme as on

    return str(on.FFMPEG_SURESI)


# ── üretim yolu: OCR kararı ve klon tuzağı ─────────────────────────────────


class TestUretimYoluOcrKarari(unittest.TestCase):
    """⛔ Video yolunda OCR çalıştırılmaz — bu BİR ÖLÇÜMÜN SONUÇUDUR.

    Ölçüm: üç sahneli video (kırmızı/yeşil/mavi, "SAHNE 1/3/5").

      · OCR yalnız 1. karede → cevap "Kare 1: Sahne 1, 6." (üç kareden biri)
      · OCR tüm karelerde, etiketli → "1. Sahne 1: 6 / 2. Sahne 3: 6 / 3. …"
        (kapsamlı ama betimleme değil, düz OCR tekrarı)
      · OCR tüm karelerde + "önce anlat" talimatı → HALÜSİNASYON
        ("bir karakterin yüzü ve elini gösteren kare" — renkli düz karelerde
         ne yüz ne el var)
      · OCR hiç → üç sahne, renkler doğru, yazılar da aktarıldı  ✅

    Buradaki testler kararı sabitler: kazanan yol OCR'siz olan.
    """

    def test_video_yolunda_ocr_kapali(self):
        self.assertFalse(pipeline._ocr_gerekli("video"))

    def test_diger_turlerde_ocr_acik(self):
        """⛔ Karar yalnız videoyu etkiler; diğer yollar değişmedi."""
        for tur in ("image", "document", "sticker", "", "audio"):
            with self.subTest(tur=tur):
                self.assertTrue(
                    pipeline._ocr_gerekli(tur),
                    f"{tur} yolunda OCR kapatılmamalı",
                )

    def test_env_ile_geri_acilabilir(self):
        """Ölçüm tek model/video/istemden alındı; sahibin önceliği açık kapı."""
        with mock.patch.dict(os.environ, {"GORSEL_VIDEO_OCR": "1"}):
            self.assertTrue(pipeline._ocr_gerekli("video"))

    def test_env_bos_veya_yanlissa_kapali(self):
        """Yanlış değer de OCR'ı AÇMAMALI: '1' dışında hiçbir şey sayılmaz."""
        for deger in ("0", "true", "evet", ""):
            with self.subTest(deger=deger):
                with mock.patch.dict(os.environ, {"GORSEL_VIDEO_OCR": deger}):
                    self.assertFalse(pipeline._ocr_gerekli("video"))


class TestKlonTuzağı(unittest.TestCase):
    """⛔ `gorsel/video.py` ÜRETİM YOLU DEĞİLDİR. Kimse çağırmıyor.

    Bugün (2026-10-06) tam buraya düşüldü: "video hattı çalışmıyor" denetimi
    bu klonda yapıldı, 224 saniyelik hata düzeltildi, düzeltme üretimi
    hiç etkilemedi. Asıl hata (`gorsel_pipeline.py` üç kareden birini
    anlatıyordu) ancak klona bakıldıktan sonra bulundu.

    Bu test, klonun bir gün "aslında bu yol kullanılıyor" diye
    düzeltilmesini engellemez ama O GÜN insanın bunu bilerek yapmasını
    sağlar: yorum burada yazılı.
    """

    def test_uretim_yolu_gorsel_pipeline_da_video_isler(self):
        """Üretim yolunda video DALI bulunmalı."""
        kaynak = Path(KOK / "gorsel_pipeline.py").read_text(encoding="utf-8")
        self.assertIn('tur == "video"', kaynak, "üretim yolunda video dalı kaybolmuş")

    def test_klon_uretim_yolunca_import_edilmiyor(self):
        """⛔ Klon, üretim yolundan import edilmemeli.

        Doğrulama yolu: import edilseydi `video` adı modül yüklemelerinde
        görünürdü. Burada dosya metni taranır — üretim yolunun klondan
        bir şey çağırdığını gösterir.
        """
        kaynak = Path(KOK / "gorsel_pipeline.py").read_text(encoding="utf-8")
        self.assertNotIn(
            "video",
            "\n".join(
                satir
                for satir in kaynak.splitlines()
                if "gorsel import" in satir or (satir.startswith("    video,"))
            ),
            "gorsel_pipeline klonu import ediyor — ya klonu sil ya da bunu bilerek yap",
        )

    def test_klon_basliginda_uyari_var(self):
        """Klonun başlığı, üretim yolu olmadığını AÇIKÇA söylemeli.

        Ölçümün kendisi bir yorum alışkanlığıdır; kod yorumu olmadan
        yaşayamaz. Bu dosya olmadan bir sonraki ölçüm aynı tuzağa düşer.
        """
        baslik = Path(KOK / "gorsel" / "video.py").read_text(encoding="utf-8")[:2000]
        self.assertIn(
            "ÜRETİM YOLU DEĞİLDİR",
            baslik,
            "klonun başlığı üretim yolu olmadığını söylemiyor — tuzak geri döner",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
