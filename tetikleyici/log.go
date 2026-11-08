// whatsapp-bridge-project/tetikleyici/log.go

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// logger, satırları hem ekrana hem dosyaya yazar.
//
// Arka planda çalışan bir araç olduğu için ekran geçip gider; dosya tek
// gerçek kayıttır. Dosya açılamazsa yalnız ekrana yazar ve uyarır — araç
// yine çalışır.
type logger struct {
	mu   sync.Mutex
	out  io.Writer
	file io.Closer
}

var std = &logger{out: io.Discard}

// openLog, günlük dosyasını açar. Yol boşse yalnız ekrana yazar.
func openLog(path string, onlyFile bool) error {
	std.mu.Lock()
	defer std.mu.Unlock()

	if std.file != nil {
		_ = std.file.Close()
		std.file = nil
	}

	var sinks []io.Writer
	if !onlyFile {
		sinks = append(sinks, os.Stderr)
	}

	if path != "" {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("günlük açılamadı (%s): %w", path, err)
		}
		std.file = file
		sinks = append(sinks, file)
	}

	switch len(sinks) {
	case 0:
		std.out = io.Discard
	case 1:
		std.out = sinks[0]
	default:
		std.out = io.MultiWriter(sinks...)
	}

	log.SetFlags(log.Ltime)
	log.SetOutput(writer{})
	return nil
}

// closeLog, günlük dosyasını kapatır.
func closeLog() {
	std.mu.Lock()
	defer std.mu.Unlock()

	if std.file != nil {
		_ = std.file.Close()
		std.file = nil
	}
	std.out = io.Discard
}

// logf, zaman damgalı tek günlük girişidir.
func logf(format string, args ...any) {
	log.Printf(format, args...)
}

// writer, stdlib logger'ının çıktısını bizim havuzumuza yönlendirir.
type writer struct{}

func (writer) Write(p []byte) (int, error) {
	std.mu.Lock()
	defer std.mu.Unlock()

	return std.out.Write(p)
}
