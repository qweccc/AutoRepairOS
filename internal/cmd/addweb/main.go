package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// jar 用獨立的 CookieJar，讓 session cookie 在多次 POST 間 persists。
var jar, _ = http.NewCookieJar(nil)

var (
	baseURL = os.Args[1]
	user    = os.Args[2]
	pwd     = os.Args[3]
)

var c = &http.Client{Timeout: 15000000000}

type step struct {
	method string
	url    string
	form   map[string]string
}

func login() error {
	c.Jar = http.DefaultClient.Jar
	resp, err := c.Post("http://"+baseURL+"/login", "application/x-www-form-urlencoded", strings.NewReader("username="+user+"&password="+pwd))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("登入失敗 (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func do(step step) (int, string, http.Header) {
	req, _ := http.NewRequest(step.method, "http://"+baseURL+step.url, strings.NewReader(urlencode(step.form)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return 0, err.Error(), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		msg = fmt.Sprintf("已重導向 %s", resp.Header.Get("Location"))
	}
	return resp.StatusCode, msg, resp.Header
}

func urlencode(m map[string]string) string {
	var b strings.Builder
	for k, v := range m {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strings.ReplaceAll(v, "+", "%2B"))
	}
	return b.String()
}

func main() {
	fmt.Println("== AutoRepairOS web 資料導入 ==")

	if err := login(); err != nil {
		fmt.Println("ERR login:", err)
		return
	}
	fmt.Println("OK login")

	fmt.Println("[1] 新增客戶")
	cs, cm, _ := do(step{"POST", "/customers", map[string]string{
		"name":                    "陳大文",
		"phone":                   "0912345001",
		"notification_preference": "line",
		"address":                 "台北市中山區南京東路三段 1 號",
		"line_user_id":            "U100001",
		"notes":                   "測試用測試資料",
	}})
	fmt.Printf("   HTTP %d | %s\n", cs, cm)
	if cs < 200 || cs >= 300 {
		fmt.Println("   客戶建立失敗，停止")
		return
	}

	fmt.Println("[2] 新增車輛")
	vs, vm, _ := do(step{"POST", "/vehicles", map[string]string{
		"customer_id":       "1",
		"plate_number":      "台北-1234-567",
		"brand_model":       "Honda CB190R",
		"manufacture_year":  "2021",
		"next_maintenance_date": "2026-12-31",
		"next_maintenance_mileage": "12000",
	}})
	fmt.Printf("   HTTP %d | %s\n", vs, vm)
	if vs < 200 || vs >= 300 {
		fmt.Println("   車輛建立失敗，停止")
		return
	}

	fmt.Println("[3] 新增工單")
	wos, wsm, hdr := do(step{"POST", "/work-orders", map[string]string{
		"vehicle_id":         "1",
		"current_mileage":    "12500",
		"customer_complaint": "引擎異音、煞車踏板偏軟",
	}})
	fmt.Printf("   HTTP %d | %s\n", wos, wsm)
	if wos < 200 || wos >= 300 {
		fmt.Println("   工單建立失敗，停止")
		return
	}
	woID := strings.TrimPrefix(hdr.Get("Location"), "/")
	fmt.Println("[3b] 更新工單狀態為 in_progress")
	do(step{"POST", "/work-orders/" + woID + "/status", map[string]string{"status": "in_progress"}})

	fmt.Println("[4] 加入工單項目（工資 + 零件）")
	do(step{"POST", "/work-orders/" + woID + "/items", map[string]string{
		"item_type":        "labor",
		"description":      "引擎診斷與檢查",
		"quantity":         "1",
		"unit_price":       "1200",
		"primary_technician_id": "3",
	}})
	do(step{"POST", "/work-orders/" + woID + "/items", map[string]string{
		"item_type":        "part",
		"inventory_item_id": "1",
		"description":      "機油 5W-30",
		"quantity":         "1",
		"unit_price":       "980",
	}})

	fmt.Println("== 完成 ==")
}
