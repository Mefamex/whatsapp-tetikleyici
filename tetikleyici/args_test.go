// whatsapp-bridge-project/tetikleyici/args_test.go

package main

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// withArgs, parseArgs'ın os.Args'i okumasını sağlar ve test sonrası eski
// hâline döndürür.
func withArgs(t *testing.T, argv ...string) args {
	t.Helper()

	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = append([]string{"tetikleyici"}, argv...)

	return parseArgs()
}

func TestActionTimeoutFlagDefaultsToThreeMinutes(t *testing.T) {
	got := withArgs(t, "--config", "x.yaml")

	if got.actionTO != 3*time.Minute {
		t.Errorf("varsayılan --action-timeout = %s, want 3m", got.actionTO)
	}
}

func TestActionTimeoutFlagIsParsed(t *testing.T) {
	got := withArgs(t, "--config", "x.yaml", "--action-timeout", "5m")

	if got.actionTO != 5*time.Minute {
		t.Errorf("--action-timeout 5m = %s, want 5m", got.actionTO)
	}
}

// opencode turları 3 dakikayı aşabiliyor; kullanıcı büyütmek isteyebilmeli.
func TestActionTimeoutFlagAcceptsColonForm(t *testing.T) {
	got := withArgs(t, "--config", "x.yaml", "--action-timeout:90s")

	if got.actionTO != 90*time.Second {
		t.Errorf("--action-timeout:90s = %s, want 90s", got.actionTO)
	}
}

// parseArgs geçersiz girdide os.Exit(2) çağırır. Bunu testin kendi
// sürecinde denemek mümkün değil (süreç ölür); standart yöntem binary'yi
// yeniden çalıştırmaktır: alt süreç beklenen çıkış koduyla kapanırsa
// doğrulama gerçekten çalışmış demektir.
func TestActionTimeoutFlagRejectsZero(t *testing.T) {
	if os.Getenv("TETIKLEYICI_ARGS_CHILD") == "1" {
		os.Args = []string{
			"tetikleyici", "--config", "x.yaml", "--action-timeout", "0",
		}
		parseArgs() // os.Exit(2) burada çağrılmalı
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestActionTimeoutFlagRejectsZero")
	cmd.Env = append(os.Environ(), "TETIKLEYICI_ARGS_CHILD=1")

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("alt süreç hata ile değil normal kapandı (hata=%v)\n%s", err, out)
	}
	if exit.ExitCode() != 2 {
		t.Errorf("çıkış kodu = %d, want 2 (geçersiz --action-timeout)\n%s", exit.ExitCode(), out)
	}
}
