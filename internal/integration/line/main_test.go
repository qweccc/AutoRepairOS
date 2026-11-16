package line

import (
	"os"
	"testing"

	"autorepairos/internal/i18n"
)

// TestMain loads the real locale dictionaries so i18n.T resolves in tests.
// StatusLine 現在走字典，未載入時 T() 會回傳 raw key。
func TestMain(m *testing.M) {
	if err := i18n.Load("../../../locales"); err != nil {
		println("i18n test dict load failed:", err.Error())
	}
	os.Exit(m.Run())
}
