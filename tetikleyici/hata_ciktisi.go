// whatsapp-bridge-project/tetikleyici/hata_ciktisi.go
//
// STDERR'i HATAYA TAŞIMA.
//
// SORUN (ölçülmüş, 2026-10-06): `cmd.Stderr = os.Stderr` idi. Bu, stderr'i
// hataya KAÇIRIRDI — hatada yalnız "exit status 1" görünüyordu. Oysa gerçek
// sebep stderr'de yazılır:
//
//	$ opencode run ...
//	API error: Rate limit exceeded          ← stderr
//	                                            (hatada YOK)
//
// Bu tek satır iki özelliği birden öldürüyordu:
//
//  1. `bosCiktiSebebi` içindeki "Rate limit" dalı: kullanıcıya
//     "Model şu an yük altında" mesajı veren kod **ölüydü**. Hiç
//     tetiklenmiyordu.
//  2. Rate limit geri çekilmesi (bekleme.go): aynı sebeple hiç kurulamıyordu.
//
// İkisi de stderr'e bakıyordu; stderr hatada olmadığı için ikisi de sessizce
// "çalışıyor" görünüyordu. Kullanıcı hiçbir sebep öğrenemez, yalnız "cevap
// gelmiyor" der.
//
// ÇÖZÜM: stderr iki yere birden yazılır — ekrana (davranış değişmez) ve son
// N baytını tutan bir tampona. Hata dönerken bu kuyruk eklenir; hem kullanıcıya
// doğru sebep gider hem de tanıma işlevleri görebilir.

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// hataCiktisiMaksBayt, hataya eklenecek stderr'in üst sınırı.
//
// Son N bayt tutulur, tümü değil. Sınırsız tutulsaydı uzun süreli bir komut
// belleği doldurabilirdi — hata ayıklamak için açılan yol, süreci çökertmemeli.
const hataCiktisiMaksBayt = 2048

// hataCiktisi, stderr'i ekrana yazarken son baytlarını da tutar.
//
// ⛔ NEDEN SON BAYT: süreç sonlandıktan sonra yazdığı son hatayı genelde
// stderr'in sonunda bırakır (yığın izi, sağlayıcı mesajı). Başı tutmak en
// değerli kısmı atabilirdi.
type hataCiktisi struct {
	son []byte
}

// Write, io.Writer'dır: MultiWriter içinde ekrana akarken bu tampona da
// yazılır. Dönüş değeri daima TAM yazılan bayt sayısıdır; MultiWriter'ın
// kısa yazma (short write) hatası üretmemesi için şart.
//
// ⛔ PARÇA TEK BAŞINA SINIRDAN BÜYÜKSE ayrı ele alınır. Genel yol
// ("baştan at, sona ekle") bu durumda dilim sınırı TAŞAR ve süreç PANIK
// eder. Test bunu yakaladı: 18 KB'lık tek bir yazma, 2 KB'lık tampona
// sığmadığı için `slice bounds out of range [20480:0]` ile çöktü.
// Gerçekçi senaryo: opencode tek seferde devasa bir hata yığını basar.
func (h *hataCiktisi) Write(p []byte) (int, error) {
	// Parça zaten sınırdan büyükse korunacak olan onun SONUDUR: eski
	// içeriğin tamamı düşer.
	if len(p) >= hataCiktisiMaksBayt {
		bas := len(p) - hataCiktisiMaksBayt
		h.son = append(h.son[:0], p[bas:]...)
		return len(p), nil
	}

	// Sığdıysa olduğu gibi ekle (yol en sıcak yol).
	if len(h.son)+len(p) <= hataCiktisiMaksBayt {
		h.son = append(h.son, p...)
		return len(p), nil
	}

	// Taşıyorsa BAŞTAN at, SONA ekle.
	//
	// kalan, p için ayrılan yer; len(h.son) > kalan olduğu için
	// len(h.son)-kalan güvenli bir dilim başlangıcıdır.
	kalan := hataCiktisiMaksBayt - len(p)
	copy(h.son, h.son[len(h.son)-kalan:])
	h.son = append(h.son[:kalan], p...)
	return len(p), nil
}

// cikti, ekrana ve tampona aynı anda yazan hedef.
func (h *hataCiktisi) cikti() io.Writer {
	return io.MultiWriter(os.Stderr, h)
}

// sonek, hataya eklenecek okunur parçayı döner. Boşsa "" — hata metnine
// boş ayraç bile eklenmez.
func (h *hataCiktisi) sonek() string {
	if h == nil {
		return ""
	}
	metin := tekCumlelik(string(h.son))
	if metin == "" {
		return ""
	}
	return ": " + metin
}

// tekCumlelik, çok satırlı stderr'i tek satırda, okunur biçimde toplar.
//
// Çok satırlı hata metni kullanıcıya gönderildiğinde sohbeti bozar ve
// tanıma işlevleri için gereksiz gürültü yaratır. Yalnız dolgu boşlukları
// atılır; kelime KESİLMEZ, çünkü "Rate limit exceeded" yarıya kadar
// kırpılırsa tanıma bozulur.
func tekCumlelik(s string) string {
	// Satır sonları ve ardışık boşluklar tek boşluğa iner.
	var b bytes.Buffer
	oncekiBosluk := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !oncekiBosluk {
				b.WriteByte(' ')
				oncekiBosluk = true
			}
			continue
		}
		b.WriteRune(r)
		oncekiBosluk = false
	}
	return b.String()
}

// cikisHatasi, sürecin hata çıkışını stderr kuyruğuyla zenginleştirir.
//
// Sarma `%w` ile yapılır: errors.Is(err, context.DeadlineExceeded) ve
// errors.As(err, &exitErr) ÇALIŞMAYA DEVAM EDER. Bu şart — zaman aşımı
// davranışı bu kontrole dayanıyor, kırılırsa kullanıcıya "zaman aşımı"
// yerine "çıkış hatası" gider.
func cikisHatasi(hata error, cikti *hataCiktisi) error {
	if hata == nil {
		return nil
	}
	ek := cikti.sonek()
	if ek == "" {
		return hata
	}
	return fmt.Errorf("%w%s", hata, ek)
}
