// whatsapp-bridge-project/tetikleyici/seen.go

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// seenStore, hangi mesaj id'lerinin işlendiğini hatırlar.
//
// Sebep: kopuk pencerede gelen mesajlar hem canlı akışta hem catch-up'ta
// görülebilir; aynı komut iki kez çalışmamalıdır.
//
// Bellekte tutulur, dosyaya yazılır. Dosya tek satırda kimlikten oluşur;
// okunamazsa uyarılır ve süreç yine çalışmaya devam eder — dedup kaybı
// çift tetiklemedir, tetikleyicinin durması değil.
type seenStore struct {
	path string

	mu   sync.Mutex
	ids  map[string]bool
	load bool
}

// newSeenStore, verilen dosyayı kullanan bir depo kurar. Hemen okumaz.
func newSeenStore(path string) *seenStore {
	return &seenStore{path: path, ids: make(map[string]bool)}
}

// alreadyHandled, mesaj daha önce işlendiyse true döner.
func (s *seenStore) alreadyHandled(id string) bool {
	if id == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoaded()

	return s.ids[id]
}

// mark, mesajı işlenmiş sayar ve listeye yazar.
func (s *seenStore) mark(id string) {
	if id == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoaded()

	if s.ids[id] {
		return
	}
	s.ids[id] = true

	if err := s.flush(); err != nil {
		logf("dedup dosyası yazılamadı (%s): %v", s.path, err)
	}
}

// count, kaç mesajın işlendiğini bildirir.
func (s *seenStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoaded()

	return len(s.ids)
}

// ensureLoaded, dosyayı ilk kullanımda okur.
//
// Çağıran zaten kilidi tutar; ayrı kilit almak deadlock'e yol açardı.
func (s *seenStore) ensureLoaded() {
	if s.load {
		return
	}
	s.load = true

	file, err := os.Open(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("dedup dosyası okunamadı (%s): %v", s.path, err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if id := strings.TrimSpace(scanner.Text()); id != "" {
			s.ids[id] = true
		}
	}
	if err := scanner.Err(); err != nil {
		logf("dedup dosyası okunamadı (%s): %v", s.path, err)
	}
}

// flush, kimlikleri dosyaya yazar. Çağıran kilidi tutar.
func (s *seenStore) flush() error {
	if s.path == "" {
		return nil
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	var b strings.Builder
	for id := range s.ids {
		b.WriteString(id)
		b.WriteByte('\n')
	}

	return os.WriteFile(s.path, []byte(b.String()), 0o600)
}
