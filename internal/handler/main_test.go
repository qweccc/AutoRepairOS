package handler

import (
	"os"
	"testing"

	"autorepairos/internal/i18n"
)

// TestMain loads the real locale dictionaries so T() resolves in handler
// tests (templ components call i18n.T during render). Failure only warns:
// i18n falls back to DefaultLang behavior without dictionaries.
func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		println("i18n test dict load failed:", err.Error())
	}
	os.Exit(m.Run())
}
