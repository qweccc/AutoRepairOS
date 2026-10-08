package handler

import (
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 客戶輸入手機號碼的方式五花八門，比對前必須先正規化，
// 否則「0912-345-678」永遠查不到「0912345678」。
func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0912345678", "0912345678"},
		{"0912-345-678", "0912345678"},
		{"0912 345 678", "0912345678"},
		{"+886 912 345 678", "0912345678"},
		{"886912345678", "0912345678"},
		{"0912345678 請幫我查", "0912345678"},
		{"我的號碼是0912345678", "0912345678"},

		// 非手機號碼或格式不對
		{"進度", ""},
		{"", ""},
		{"123", ""},
		{"02-2345-6789", ""}, // 台北市號,不是手機
		{"09123456789", ""},  // 11 碼,過長
		{"1912345678", ""},   // 不是 09 開頭
		{"不確定", ""},
	}
	for _, c := range cases {
		if got := normalizePhone(c.in); got != c.want {
			t.Errorf("normalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 綁定流程：回報手機號碼後應自動連結到對應客戶。
func TestBindByPhoneLinksCustomer(t *testing.T) {
	setupLineWebhook(t, testSecret)

	db.MustExec(`INSERT INTO customers(name, phone) VALUES ('王小姐','0912345678')`)

	ev := messageEventForTest("U_neww", "0912-345-678")
	reply, handled := tryBindByPhone(nil, ev)
	if !handled {
		t.Fatal("回報手機號碼應被視為綁定嘗試")
	}

	var lineID string
	db.DB.QueryRow("SELECT line_user_id FROM customers WHERE phone='0912345678'").Scan(&lineID)
	if lineID != "U_neww" {
		t.Errorf("LINE ID 未綁定，得到 %q", lineID)
	}
	if !strings.Contains(reply, "王小姐") {
		t.Errorf("回覆應包含客戶名稱，得到 %q", reply)
	}
	t.Logf("綁定成功回覆: %s", reply)
}

// 查無此號碼時不能建立任何綁定。
func TestBindByPhoneRejectsUnknown(t *testing.T) {
	setupLineWebhook(t, testSecret)

	reply, handled := tryBindByPhone(nil, messageEventForTest("U_x", "0900000000"))
	if !handled {
		t.Fatal("應被視為綁定嘗試並回覆錯誤")
	}
	if !strings.Contains(reply, "查無") {
		t.Errorf("應提示查無號碼，得到 %q", reply)
	}
	var bound int
	db.DB.QueryRow("SELECT COUNT(1) FROM customers WHERE line_user_id = 'U_x'").Scan(&bound)
	if bound != 0 {
		t.Error("查無號碼時不應建立任何綁定")
	}
}

// phone 有 UNIQUE 約束，所以「同一門號多客戶」在資料庫層不可能發生。
// 這裡測試確認該前提成立——若哪天拿掉 UNIQUE，ambiguous 分支就會生效，
// 這個測試會提醒我們重新評估。
func TestPhoneIsUniqueSoAmbiguityImpossible(t *testing.T) {
	setupLineWebhook(t, testSecret)

	db.MustExec(`INSERT INTO customers(name, phone) VALUES ('王小姐','0912345678')`)
	if _, err := db.DB.Exec(`INSERT INTO customers(name, phone) VALUES ('王先生','0912345678')`); err == nil {
		t.Fatal("phone 有 UNIQUE 約束，重複號碼應被資料庫拒絕")
	}
	if n := customerCountByPhone("0912345678"); n != 1 {
		t.Errorf("號碼應只對應 1 個客戶，得到 %d", n)
	}
}

// 已綁定的使用者再傳訊息不應被綁定流程攔截，要走正常查詢。
func TestBindSkipsAlreadyBound(t *testing.T) {
	setupLineWebhook(t, testSecret)

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('李先生','0911000000','U_bound')`)

	reply, handled := tryBindByPhone(nil, messageEventForTest("U_bound", "0912345678"))
	if handled {
		t.Error("已綁定者不應再走綁定流程")
	}
	if reply != "" {
		t.Errorf("已綁定者應回空字串交由正常流程，得到 %q", reply)
	}
}

// 防止綁定被盜用：同一客戶不能被兩個 LINE 帳號綁定。
func TestBindLineUserRejectsHijack(t *testing.T) {
	setupLineWebhook(t, testSecret)

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('王小姐','0912000000','U_first')`)

	err := bindLineUser("U_second", 1)
	if err == nil {
		t.Error("客戶已被綁定時應拒絕第二次綁定")
	} else {
		t.Logf("拒絕原因: %v", err)
	}

	var lineID string
	db.DB.QueryRow("SELECT line_user_id FROM customers WHERE id=1").Scan(&lineID)
	if lineID != "U_first" {
		t.Errorf("原本綁定被覆寫: %s", lineID)
	}
}

// 反向：同一 LINE 帳號不能綁到第二個客戶。
func TestBindLineUserRejectsSameLineTwoCustomers(t *testing.T) {
	setupLineWebhook(t, testSecret)

	db.MustExec(`INSERT INTO customers(name, phone, line_user_id) VALUES ('甲','0913000000','U_dup')`)
	db.MustExec(`INSERT INTO customers(name, phone) VALUES ('乙','0913000001')`)

	if err := bindLineUser("U_dup", 2); err == nil {
		t.Error("同一 LINE 帳號應只能綁定一個客戶")
	}
}

// 舊系統匯入的電話帶分隔符與續號，必須也能比對到。
//
// 這是實際踩到的 bug：normalizePhone 只正規化「使用者輸入」，
// 但 customers.phone 存的是舊系統原樣。實測 6,512 筆電話中
// 2,746 筆是「0905-121161」這種格式、1,201 筆帶「#1」後綴，
// 用 phone = ? 直接比對會全部落空，客戶回報號碼一律得到「查無此號碼」。
//
// 每個格式用不同門號，避免觸發「同號多客戶 → 需人工綁定」的保護
// （那個保護本身是對的，另有測試涵蓋）。
func TestBindByPhoneMatchesLegacyFormats(t *testing.T) {
	setupLineWebhook(t, testSecret)

	legacy := []struct {
		stored string
		digits string // 使用者會輸入的乾淨形式
		note   string
	}{
		{"0912345678", "0912345678", "乾淨格式"},
		{"0922-345-678", "0922345678", "帶破折號（最常見）"},
		{"0933 345 678", "0933345678", "帶空白"},
		{"0944345678#1", "0944345678", "帶續號（同門號第二位聯絡人）"},
	}
	for i, c := range legacy {
		id := int64(1000 + i)
		db.MustExec(`INSERT INTO customers(id, name, phone) VALUES (?,?,?)`,
			id, "客戶"+c.note, c.stored)

		ev := messageEventForTest("U_"+c.note, c.digits)
		reply, handled := tryBindByPhone(nil, ev)
		if !handled {
			t.Errorf("%s：應被視為綁定嘗試", c.note)
			continue
		}
		var got string
		db.DB.QueryRow("SELECT line_user_id FROM customers WHERE id = ?", id).Scan(&got)
		if got == "" {
			t.Errorf("%s（存 %q，輸入 %q）：綁定失敗，回覆=%q",
				c.note, c.stored, c.digits, reply)
		}
	}
}
