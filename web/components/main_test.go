package components

import (
	"os"
	"testing"

	"autorepairos/internal/i18n"
)

// TestMain loads the real locale dictionaries so i18n.T resolves in tests.
// Pager 等元件現在走字典，未載入時 T() 會回傳 raw key，測試斷言的「下一頁」就會失敗
// （看起來像功能壞了，其實只是字典沒載入）。
func TestMain(m *testing.M) {
	if err := i18n.Load("../../locales"); err != nil {
		println("i18n test dict load failed:", err.Error())
	}
	os.Exit(m.Run())
}
