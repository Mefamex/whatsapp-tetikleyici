#!/usr/bin/env python3
"""cmd.py — /cmd ve /session giriş noktası.

tetikleyici.yaml'dan çağrılan ana betik. Mesajı ayrıştırır, yetki
kontrolü yapar, komutu çalıştırır ve sonucu stdout'a basar.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.request
from pathlib import Path

# Proje modülleri
sys.path.insert(0, str(Path(__file__).resolve().parent))
from sozdizimi import parse_cmd, CmdParseResult
from yetki import yetkili_mi, yetki_red_mesaji
from calistir import calistir, CalistirSonuc
from cikti import yaz, CIKTI_KLASORU, cikti_kirp
from zamanlayici import (
    gorev_baslat,
    gorev_durdur,
    tum_gorevleri_durdur,
    gorev_listele,
)
from yardim import yardim_al, kisa_yardim


# WhatsApp mesaj boyutu sınırı (güvenli marjla)
WHATSAPP_MAKS_UZUNLUK = 3500

# opencode API base URL
OPENCODE_API_BASE = "http://127.0.0.1:4096"  # varsayılan opencode port


def opencode_api_yol(yol: str) -> str:
    """opencode API tam URL'sini oluştur."""
    return f"{OPENCODE_API_BASE}{yol}"


def opencode_istek(yol: str, method: str = "GET", veri: dict = None) -> dict:
    """opencode API'ye istek at."""
    url = opencode_api_yol(yol)
    basliklar = {"Content-Type": "application/json"}

    if veri is not None:
        veri_bytes = json.dumps(veri).encode("utf-8")
    else:
        veri_bytes = None

    istek = urllib.request.Request(
        url, data=veri_bytes, headers=basliklar, method=method
    )

    try:
        with urllib.request.urlopen(istek, timeout=10) as yanit:
            return json.loads(yanit.read().decode("utf-8"))
    except urllib.error.HTTPError as e:
        return {"hata": f"HTTP {e.code}: {e.read().decode('utf-8')}"}
    except Exception as e:
        return {"hata": str(e)}


from session_manager import (
    session_exit,
    session_go,
    session_listele,
    session_state,
    session_stop,
)
from cmdhub import cmdhub_calistir, cmdhub_yonet
from tunel import kullanici_modu, kullanici_tunel_sifirla


def kamera_izni_acik() -> bool:
    """tetikleyici.yaml'da `kamera_izni: true` yazıyor mu?

    Bağımlılıksız okuma: yalnızca ilgili satıra bakılır (yorum satırları
    atlanır). Dosya okunamazsa KAPALI sayılır (şüphede geç — madde 29).
    """
    import re

    config_yolu = os.getenv("TRIGGER_CONFIG", "").strip()
    if not config_yolu:
        # cmd.py'nin yanından iki üst dizin = proje kökü
        config_yolu = str(
            Path(__file__).resolve().parent.parent.parent / "tetikleyici.yaml"
        )
    try:
        with open(config_yolu, encoding="utf-8") as f:
            for satir in f:
                s = satir.strip()
                if s.startswith("#"):
                    continue
                m = re.match(r"^kamera_izni\s*:\s*(\S+)", s)
                if m:
                    return m.group(1).lower() in ("true", "yes", "on")
    except OSError:
        pass
    return False


def _aktif_motor_yolu() -> Path:
    state_env = os.getenv("TETIKLEYICI_STATE_DIR", "").strip()
    if state_env:
        return Path(state_env) / "aktif_motor.txt"
    trigger_config = os.getenv("TRIGGER_CONFIG", "").strip()
    if trigger_config:
        return Path(trigger_config).resolve().parent / ".tetikleyici" / "aktif_motor.txt"
    return Path(__file__).resolve().parent.parent.parent / ".tetikleyici" / "aktif_motor.txt"


def agent_yonet(kod: str) -> str:
    yol = _aktif_motor_yolu()
    yol.parent.mkdir(parents=True, exist_ok=True)

    if kod == "status":
        motor = "agy"
        if yol.is_file():
            icerik = yol.read_text(encoding="utf-8").strip().lower()
            if icerik in ("agy", "claude", "opencode"):
                motor = icerik
        return (
            f"🤖 Aktif Ajan Motoru: *{motor}*\n\n"
            f"Desteklenen motorlar:\n"
            f"  • agy {'(aktif) ✅' if motor == 'agy' else ''}\n"
            f"  • claude {'(aktif) ✅' if motor == 'claude' else ''}\n"
            f"  • opencode {'(aktif) ✅' if motor == 'opencode' else ''}\n\n"
            f"Değiştirmek için: /agent <motor>\n"
            f"Örnek: /agent agy"
        )

    yol.write_text(kod.lower() + "\n", encoding="utf-8")
    return f"✅ Aktif ajan motoru değiştirildi: *{kod.lower()}*\nGelen mesajlar artık *{kod.lower()}* motoruna iletilecek."


def ana(metin: str, gonderen_numara: str, gonderen_isim: str) -> str:
    """Ana giriş fonksiyonu.

    Args:
        metin: Gelen mesaj metni (/cmd ... veya /help)
        gonderen_numara: Gönderen telefon/LID
        gonderen_isim: Gönderen push_name

    Returns:
        WhatsApp'a gönderilecek cevap metni
    """
    # Yetki kontrolü (ikinci katman)
    if not yetkili_mi(gonderen_numara, gonderen_isim):
        return yetki_red_mesaji()

    # DEBUG_CIKTI: opencode'nin ham stdout/stderr'ini gönder
    debug_cikti = os.getenv("DEBUG_CIKTI", "false").lower() == "true"

    metin = metin.strip()

    # /help
    if metin.lower() == "/help":
        return kisa_yardim()

    # Komutu ayrıştır
    sonuc = parse_cmd(metin)

    # /exit — her türlü tünelden (session veya cmdhub) hızlı çıkış
    if sonuc.komut_turu == "exit":
        kullanici_tunel_sifirla(gonderen_numara)
        return "🚪 Tünelden çıkıldı. Normal ana oturuma dönüldü."

    if sonuc.komut_turu == "invalid":
        # CMDHUB modunda slash ile başlamayan metinler doğrudan kabuk komutu sayılır
        if kullanici_modu(gonderen_numara) == "cmdhub":
            return cmdhub_calistir(metin, gonderen_numara, debug_cikti)
        return f"Hata: {sonuc.hata}\n\n{kisa_yardim()}"

    if sonuc.komut_turu == "help":
        return kisa_yardim()

    # /ping — pong. Ajan turu bekletmez; tetikleyici sağlığını anında ölçer.
    if sonuc.komut_turu == "ping":
        return "pong 🏓"

    # /sil veya /revoke — alıntılanan mesajı herkesten sil
    if sonuc.komut_turu == "revoke":
        import json
        import urllib.request

        target_id = (
            sonuc.hedef
            or os.getenv("TRIGGER_QUOTED_ID", "")
            or os.getenv("TRIGGER_ID", "")
        )
        if not target_id:
            return "Hata: Silinecek mesaj bulunamadı (silmek istediğin mesaja sağa çekip /sil yazabilirsin)."

        url = "http://127.0.0.1:31415/revoke"
        data = json.dumps({"to": gonderen_numara, "message_id": target_id}).encode(
            "utf-8"
        )
        req = urllib.request.Request(
            url,
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                if resp.status == 200:
                    return "🗑️ Mesaj silindi."
                return f"Köprü hatası: {resp.status}"
        except Exception as e:
            return f"Mesaj silinemedi: {e}"

    # /agent — aktif ajan motorunu göster / değiştir (agy, claude, opencode)
    if sonuc.komut_turu == "agent":
        return agent_yonet(sonuc.kod)

    # /session list [n]
    if sonuc.komut_turu == "session_list":
        return session_listele(sonuc.adet)

    # /session go <hedef> (veya /session choose <n>)
    if sonuc.komut_turu in ("session_go", "session_choose"):
        return session_go(sonuc.hedef or sonuc.kod, gonderen_numara)

    # /session exit
    if sonuc.komut_turu == "session_exit":
        return session_exit(gonderen_numara)

    # /session stop
    if sonuc.komut_turu == "session_stop":
        return session_stop(gonderen_numara)

    # /session state / status
    if sonuc.komut_turu == "session_state":
        return session_state(gonderen_numara)

    # /cmdhub [alt_komut]
    if sonuc.komut_turu == "cmdhub":
        return cmdhub_yonet(
            sonuc.alt_komut, sonuc.hedef or sonuc.kod, gonderen_numara, debug_cikti
        )

    # /call — giden arama (çaldırma)
    if sonuc.komut_turu == "call":
        import json
        import urllib.request

        target = sonuc.kod
        duration = sonuc.saniye

        # Köprü /call endpoint'ini çağır
        url = "http://127.0.0.1:31415/call"
        data = json.dumps({"to": target, "duration_sec": duration}).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=10) as resp:
                yanit = json.loads(resp.read().decode("utf-8"))
            if yanit.get("ok"):
                return f"📞 Arama başlatıldı: {yanit.get('to')} ({yanit.get('duration_sec')}sn)"
            return f"❌ Arama hatası: {yanit.get('error', 'bilinmeyen')}"
        except Exception as e:
            return f"❌ Köprü erişilemedi: {e}"

    # /camera — çift kapılı: yetki (yukarıda) + kamera_izni (config)
    if sonuc.komut_turu == "camera":
        if not kamera_izni_acik():
            return (
                "Kamera kapalı. Açmak için config'de `kamera_izni: true` yaz "
                "(tetikleyici.yaml AYARLAR)."
            )
        from kamera import ana as kamera_ana

        return kamera_ana()

    # /durum — salt okunur, izin gerekmez (yetki zaten yukarıda)
    if sonuc.komut_turu == "durum":
        from durum import ana as durum_ana

        return durum_ana()

    # /zamanla — "şu zaman sonra şu mesajı gönder" (config kapısı içeride)
    if sonuc.komut_turu == "zamanla":
        from zamanla import zamanla_komut

        return zamanla_komut(sonuc.zaman_metni, sonuc.metin, gonderen_numara)

    # /reply — bekleyen soruya cevap (ADIM 1: komut + kayıt mekanizması)
    if sonuc.komut_turu == "reply":
        from reply import reply_komut

        return reply_komut(sonuc.metin, sonuc.numara)

    if sonuc.komut_turu == "list":
        gorevler = gorev_listele()
        if not gorevler:
            return "Aktif tekrarlayıcı yok."
        satirlar = ["Aktif tekrarlayıcılar:"]
        for g in gorevler:
            satirlar.append(
                f"  #{g['id']}: her {g['aralik_saniye']}sn · {g['calisma_sayisi']} tur · "
                f"son {g['son_calisma_sn_once']}sn önce · {g['dosya_adi']}\n"
                f"      {g['kod']}"
            )
        return "\n".join(satirlar)

    if sonuc.komut_turu == "stop_all":
        sayi = tum_gorevleri_durdur()
        return f"Tüm tekrarlayıcılar durduruldu ({sayi} adet)."

    if sonuc.komut_turu == "stop":
        try:
            gorev_id = int(sonuc.kod)
        except ValueError:
            return "Hata: Geçersiz ID. Kullanım: /cmd stop <id>"
        if gorev_durdur(gorev_id):
            return f"Tekrarlayıcı #{gorev_id} durduruldu."
        return f"Tekrarlayıcı #{gorev_id} bulunamadı."

    # Tek seferlik veya tekrarlayan komut
    kod = sonuc.kod

    # Çıktı callback'i: her turda WhatsApp'a gönderilecek metni üretir
    def cikti_callback(cikti: str):
        # Bu callback tetikleyici tarafından kullanılacak
        # Burada sadece loglayalım, asıl gönderim tetikleyici tarafında
        pass

    if sonuc.komut_turu == "once":
        # Tek seferlik çalıştır
        calistir_sonuc = calistir(kod, debug_cikti=debug_cikti)

        # Çıktıyı birleştir
        cikti_parcalar = []
        cikti_parcalar.append(f"Komut: {kod}")
        cikti_parcalar.append(f"Çıkış kodu: {calistir_sonuc.cikis_kodu}")
        if calistir_sonuc.zaman_asimi:
            cikti_parcalar.append(f"⚠ Zaman aşımı")
        if calistir_sonuc.hata:
            cikti_parcalar.append(f"Hata: {calistir_sonuc.hata}")

        if debug_cikti:
            # Debug modu: ham stdout/stderr'yi gönder (kırpma)
            if calistir_sonuc.stdout:
                cikti_parcalar.append(
                    f"--- STDOUT ---\n{calistir_sonuc.stdout.strip()}"
                )
            if calistir_sonuc.stderr:
                cikti_parcalar.append(
                    f"--- STDERR ---\n{calistir_sonuc.stderr.strip()}"
                )
        else:
            # Normal mod: kırpılmış çıktı
            if calistir_sonuc.stdout:
                cikti_parcalar.append(
                    f"--- STDOUT ---\n{calistir_sonuc.stdout.strip()}"
                )
            if calistir_sonuc.stderr:
                cikti_parcalar.append(
                    f"--- STDERR ---\n{calistir_sonuc.stderr.strip()}"
                )

        cikti = "\n".join(cikti_parcalar)

        # Dosyaya yaz
        dosya_yolu = yaz(cikti + "\n")
        cikti_parcalar.append(f"\nDosya: {dosya_yolu.name}")
        cikti_parcalar.append("cmd done")

        tam_cikti = "\n".join(cikti_parcalar)
        return cikti_kirp(tam_cikti) if not debug_cikti else tam_cikti

    elif sonuc.komut_turu == "repeat":
        # Tekrarlayan görev başlat
        gorev = gorev_baslat(kod, sonuc.saniye, cikti_callback)
        return (
            f"Tekrarlayıcı başlatıldı: #{gorev.id}\n"
            f"Komut: {kod}\n"
            f"Aralık: {sonuc.saniye} saniye\n"
            f"Çıktı dosyası: {gorev.dosya_adi}\n"
            f"Durdurmak için: /cmd stop {gorev.id}"
        )

    return "Bilinmeyen komut türü."


if __name__ == "__main__":
    # Test modu: python3 cmd.py "<komut>" "<numara>" "<isim>"
    if len(sys.argv) >= 2:
        test_metin = sys.argv[1]
        test_numara = sys.argv[2] if len(sys.argv) > 2 else "905550000001"
        test_isim = sys.argv[3] if len(sys.argv) > 3 else "Mefamex"
        print(ana(test_metin, test_numara, test_isim))
    else:
        # Etkileşimli test
        print("cmd.py test modu")
        print("Komut girin (Çıkış: Ctrl+C):")
        while True:
            try:
                metin = input("> ")
                print(ana(metin, "905550000001", "Mefamex"))
                print("---")
            except (EOFError, KeyboardInterrupt):
                break
