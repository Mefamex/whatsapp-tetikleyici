#!/usr/bin/env bash
# tetikleyici.sh — tetikleyici'yi elle çalıştırmak için ince sarmalayıcı.
#
# Servis yok; süreç arka planda nohup ile yaşar. PID dosyada tutulur.
#
#   ./tetikleyici.sh start      arka planda başlat
#   ./tetikleyici.sh stop       durdur
#   ./tetikleyici.sh restart    durdur + başlat
#   ./tetikleyici.sh status     çalışıyor mu, kaç kural var, son log
#   ./tetikleyici.sh log [N]    son N satır günlük (varsayılan 40)
#   ./tetikleyici.sh run        ön planda çalıştır (Ctrl-C ile dur)
#   ./tetikleyici.sh clean      eski yusuf/monitor artıklarını sil
set -u

PROJE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$HOME/bin/tetikleyici"
CONFIG="${TETIKLEYICI_CONFIG:-$PROJE/tetikleyici.yaml}"
STATE="${TETIKLEYICI_STATE_DIR:-$PROJE/.tetikleyici}"
PIDFILE="$STATE/tetikleyici.pid"
LOGFILE="$STATE/tetikleyici.log"
OUT="$STATE/nohup.out"

if [ ! -f "$CONFIG" ] && [ -f "$PROJE/tetikleyici.example.yaml" ]; then
	echo "==> $CONFIG bulunamadı, tetikleyici.example.yaml kopyalanıyor..."
	cp "$PROJE/tetikleyici.example.yaml" "$CONFIG"
fi

if [ ! -f "$PROJE/pasif-kurallar.yaml" ] && [ -f "$PROJE/pasif-kurallar.example.yaml" ]; then
	echo "==> pasif-kurallar.yaml bulunamadı, pasif-kurallar.example.yaml kopyalanıyor..."
	cp "$PROJE/pasif-kurallar.example.yaml" "$PROJE/pasif-kurallar.yaml"
fi

mkdir -p "$STATE"

# calisiyor, çalışan tetikleyici süreçlerinin PID'lerini listeler.
# TEK'ten fazlaysa (çift örnek) alt alta yazar; çağıranlar bunu bilmeli.
#
# pgrep -f KULLANILMAZ: -f komut satırının tamamına bakar ve bu scriptin
# kendi kabuğunu da eşleştirir ("~/bin/tetikleyici" kelimesi geçtiği için).
# Böylece "çalışıyor" sanıp hiçbir şey başlatmayan bir döngüye girilir.
#
# Bunun yerine /proc/<pid>/exe sembolik bağına bakılır.
#
# ⛔ "(deleted)" EKI ATILIR (2026-10-06 02:28 ölçümü).
#   Tetikleyici yeniden derlenince çalışan ESKİ sürecin bağı
#   "~/bin/tetikleyici (deleted)" olur: inode unlink edilmiştir. Ham
#   yol karşılaştırması bu yüzden "çalışmıyor" der ve `start` ikinci bir
#   örnek başlatır. Ölçülen sonuç: iki motor aynı config'de → çift gönderim
#   bastırması iki ayrı bellekte tutulur → AYNI mesaj için iki cevap.
#   Kök neden: go build -o ~/bin/tetikleyici . (02:28:24), ikinci süreç 02:28:33.
calisiyor() {
	local d exe hedef bulundu=""
	hedef="$(readlink -f "$BINARY" 2>/dev/null)"
	[ -n "$hedef" ] || return 1
	for d in /proc/[0-9]*; do
		exe="$(readlink "$d/exe" 2>/dev/null)" || continue
		exe="${exe% (deleted)}"
		[ "$exe" = "$hedef" ] || continue
		bulundu=1
		printf '%s\n' "${d#/proc/}"
	done
	# DÖNGÜ SONUNUN DURUMU DÖNMEMELİ.
	#
	# Hata (2026-10-05 reboot sonrası): hiç süreç bulunamayınca döngüdeki son
	# komut `|| continue` olduğu için 0 dönüyordu. `status` bunu "PID var"
	# sanıp "PID — PID dosyası eski" yazıyordu; oysa tetikleyici hiç
	# çalışmıyordu. Bulunan vardır/yoktur açıkça söylenmeli.
	[ -n "$bulundu" ] || return 1
	return 0
}

# ilk_pid, çalışan örnekler arasındaki EN ESKİ PID'yi döndürür.
# Birden fazla örnek varsa bu tek bir tanesini seçer — pid_al bunu tek
# PID bekleyecek şekilde yazdığı için çok satırlı değer yazılamaz.
ilk_pid() {
	local pidler="$1" p en=""
	while read -r p; do
		[ -n "$p" ] || continue
		# en eski = en küçük başlangıç zamanı (etimes elde edilemezse PID'e bak)
		if [ -z "$en" ] || [ "$p" -lt "$en" ]; then
			en="$p"
		fi
	done <<<"$pidler"
	printf '%s\n' "$en"
}

# pid_al, PID dosyasındaki kaydın hâlâ geçerli bir tetikleyici olduğunu doğrular.
# Tek PID bekler; pid_al'ı çağıran yerler "$(<"$PIDFILE")" ile karşılaştırır.
pid_al() {
	[ -s "$PIDFILE" ] || return 1
	local pid
	pid="$(head -n1 "$PIDFILE" 2>/dev/null)"
	[ -n "$pid" ] || return 1
	kill -0 "$pid" 2>/dev/null || return 1
	case "$(calisiyor)" in
	*"$pid"*) printf '%s\n' "$pid" ;;
	*) return 1 ;;
	esac
}

baslat() {
	if pid_al >/dev/null; then
		echo "zaten çalışıyor (PID $(pid_al))"
		return 0
	fi
	local pid pidler
	pidler="$(calisiyor || true)"
	if [ -n "$pidler" ]; then
		pid="$(ilk_pid "$pidler")"
		echo "$pid" >"$PIDFILE"
		echo "zaten çalışıyor (PID $pid) — PID dosyası onarıldı"
		return 0
	fi

	[ -x "$BINARY" ] || {
		echo "hata: $BINARY yok. Derle: (cd $PROJE/tetikleyici && go build -o $BINARY .)"
		return 1
	}

	setsid -f "$BINARY" --config "$CONFIG" >>"$OUT" 2>&1
	sleep 0.4
	pid="$(calisiyor || true)"
	if [ -n "$pid" ]; then
		ilk_pid "$pid" >"$PIDFILE"
		echo "başladı (PID $(<"$PIDFILE"))  config: $CONFIG"
	else
		echo "başlatılamadı. Şuraya bak:"
		echo "  tail -20 $OUT"
		return 1
	fi
}

# ⛔ TÜM örnekleri durdurur, TEK olanını değil (2026-10-06 02:35 ölçümü).
#   Ölçülen hata: iki motor çalışırken `stop` "durdu" yazıyordu ama hiçbir
#   şeyi durdurmuyordu. Sebep: kill "$pid" tırnaklıydı, $pid çok satırlıydı
#   ("75127\n105940") ve kill bunu TEK argüman sayıp sessizce başarısız oluyordu.
#   Sonuç: `restart` ikinci bir örnek bırakıyor, `stop` hiçbir şeyi durdurmuyor.
durdur() {
	local pids pid n=0 kalan=""
	pids="$(calisiyor || true)"
	if [ -z "$pids" ]; then
		echo "çalışmıyor"
		rm -f "$PIDFILE"
		return 0
	fi

	while read -r pid; do
		[ -n "$pid" ] || continue
		kill "$pid" 2>/dev/null
		n=$((n + 1))
	done <<<"$pids"

	# SIGTERM'i dinlemeyenleri bekle, sonra zorla.
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		[ -n "$(calisiyor || true)" ] || break
		sleep 0.3
	done
	kalan="$(calisiyor || true)"
	if [ -n "$kalan" ]; then
		while read -r pid; do
			[ -n "$pid" ] || continue
			echo "SIGTERM dinlemedi, SIGKILL: $pid"
			kill -9 "$pid" 2>/dev/null
		done <<<"$kalan"
		sleep 0.5
	fi

	rm -f "$PIDFILE"
	kalan="$(calisiyor || true)"
	if [ -n "$kalan" ]; then
		# ⛔ "durdu" demek burada YANLIŞ olurdu. Gerçeği yaz.
		echo "⛔ $n sinyal gönderildi ama HÂLÂ çalışıyor:"
		printf '%s\n' "$kalan" | sed 's/^/  PID /'
		return 1
	fi
	echo "durdu ($n süreç)"
}

durum() {
	echo "config : $CONFIG"
	echo "binary : $BINARY"
	echo "günlük : $LOGFILE"
	echo

	# Çok satırlı $pid'yi olduğu gibi yazmak çıktıyı bozduğu için ilk_pid kullanılır
	# ve kaç örnek olduğu AYRICA bildirilir — çift motor sessizce normal görünürdü.
	local pid pids adet
	pids="$(calisiyor || true)"
	adet=0
	[ -n "$pids" ] && adet="$(printf '%s\n' "$pids" | grep -c '[0-9]')"

	if pid="$(pid_al)"; then
		echo "durum  : ÇALIŞIYOR (PID $pid, başlangıç $(ps -o lstart= -p "$pid" 2>/dev/null | xargs))"
	elif [ -n "$pids" ]; then
		pid="$(ilk_pid "$pids")"
		echo "durum  : ÇALIŞIYOR (PID $pid — PID dosyası bozuk, süreç /proc'tan bulundu)"
	else
		echo "durum  : DURMUŞ"
		return 1
	fi

	if [ "$adet" -gt 1 ]; then
		echo "⛔ UYARI: $adet tetikleyici örneği çalışıyor — çift cevap riski:"
		printf '%s\n' "$pids" | sed 's/^/  PID /'
		echo "  düzelt: ./tetikleyici.sh restart"
	fi

	echo "köprü  : $(curl -s --max-time 3 http://127.0.0.1:31415/health 2>/dev/null | head -c 200 || echo 'ulaşılamıyor')"
	echo
	echo "--- kurallar (config) ---"
	grep -nE '^\s*-\s*name:' "$CONFIG" 2>/dev/null || echo "(okunamadı)"
	echo
	echo "--- son 15 log ---"
	tail -15 "$LOGFILE" 2>/dev/null || echo "(günlük yok)"
}

temizle() {
	echo "eski tetikleyici artıkları:"
	for f in \
		"$STATE/payload-yusuf.json" \
		"$STATE/seen-yusuf.txt" \
		"$STATE/tetikleyici-yusuf.log" \
		"$STATE/yusuf-opencode.log" \
		"$STATE/yusuf-yanit.sh"; do
		if [ -e "$f" ]; then
			rm -f "$f"
			echo "  silindi: $f"
		fi
	done
}

case "${1:-status}" in
start) baslat ;;
stop) durdur ;;
restart)
	durdur
	baslat
	;;
status) durum ;;
log) tail -n "${2:-40}" "$LOGFILE" ;;
run) exec "$BINARY" --config "$CONFIG" ;;
clean) temizle ;;
*)
	echo "kullanım: $0 {start|stop|restart|status|log [N]|run|clean}"
	exit 2
	;;
esac
