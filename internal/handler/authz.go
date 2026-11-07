package handler

import (
	"net/http"

	"autorepairos/internal/model"
)

// 角色權限模型：
//
//	owner       系統管理員 — 全部權限，含帳號、刪除、系統維護、排程與備份
//	counter     櫃檯人員   — 開單、報價、結帳、客戶/車輛/庫存日常操作
//	technician  技師       — 讀取工單與照片、替自己記工時；不可改單、結帳、刪除
//
// 設計原則：預設拒絕。每個 route 必須顯式挑一個 wrapper，
// 避免「忘了加權限就變成全開放」。敏感欄位（成本、營收、佣金）另外用
// canSeeMoney 在頁面層隱藏，路由層只是粗篩。

// ownerOnly 只有系統管理員能用：刪除、系統維護、排程與備份。
func OwnerOnly(fn http.HandlerFunc) http.HandlerFunc {
	return requireRoles(fn, "owner")
}

// counterUp 櫃檯與管理員：日常營運（開立工單、結帳、客戶車輛、庫存調度）。
func CounterUp(fn http.HandlerFunc) http.HandlerFunc {
	return requireRoles(fn, "owner", "counter")
}

// allStaff 三種角色都能讀取頁面（內容由各頁自行依角色隱藏敏感欄位）。
// isOwnerOrCounter checks if the role is owner or counter.
func isOwnerOrCounter(role string) bool {
	return role == "owner" || role == "counter"
}

func AllStaff(fn func(http.ResponseWriter, *http.Request, model.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r, user)
	}
}

// CounterUpPage 給需要「櫃檯以上」且要拿到登入者身分的頁面 handler。
// AllStaff 讓技師也能看，但開立工單屬於櫃檯業務，因此另給一個帶 user 的版本。
func CounterUpPage(fn func(http.ResponseWriter, *http.Request, model.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if !isOwnerOrCounter(user.Role) {
			http.Error(w, "權限不足：開立工單僅限 櫃檯人員、系統管理員 使用", http.StatusForbidden)
			return
		}
		fn(w, r, user)
	}
}

// Actor 是目前登入者的最小身分資訊，給需要「用自己的身分寫入」的 handler。
type Actor struct {
	ID   int64
	Role string
}

func (a Actor) asUser() model.User { return model.User{ID: a.ID, Role: a.Role} }

// CanPickTechnician 為 true 代表可代其他技師登錄（櫃檯與管理員）；
// 技師為 false，只能替自己記工時。
func (a Actor) CanPickTechnician() bool { return canSeeMoney(a.asUser()) }

// withActor 擋未登入者，並把登入者身分注入給 handler。
// 用於「技師只能替自己操作」的場合，避免信任表單送來的 user id。
func WithActor(fn func(http.ResponseWriter, *http.Request, Actor)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r, Actor{ID: user.ID, Role: user.Role})
	}
}

// requireRoles 擋未登入者，並檢查角色是否在允許清單內。
func requireRoles(fn http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		for _, role := range roles {
			if user.Role == role {
				fn(w, r)
				return
			}
		}

		http.Error(w, "權限不足：此功能僅限 "+roleList(roles)+" 使用", http.StatusForbidden)
	}
}

func roleList(roles []string) string {
	label := map[string]string{
		"owner":      "系統管理員",
		"counter":    "櫃檯人員",
		"technician": "技師",
	}
	out := ""
	for i, r := range roles {
		if i > 0 {
			out += "、"
		}
		if l, ok := label[r]; ok {
			out += l
		} else {
			out += r
		}
	}
	return out
}

// canSeeMoney 判斷該角色是否可看到成本價、營收與佣金等財務數字。
// 技師不看這些——由 handler 在產生頁面時呼叫。
func canSeeMoney(user model.User) bool {
	return user.Role == "owner" || user.Role == "counter"
}
