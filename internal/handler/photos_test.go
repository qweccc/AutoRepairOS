package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
)

var validPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x62, 0x6F, 0xF8, 0xFF, 0xFF, 0x3F, 0x00, 0x00, 0x05, 0x2E, 0x18, 0xD3, 0x1E, 0x34, 0x5D, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func newTestServer(t *testing.T) (http.Handler, int64) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:          "file:" + dir + "/test.db?_journal_mode=WAL&_busy_timeout=5000",
		RunSeed:         true,
		DefaultOwner:    "owner",
		DefaultOwnerPwd: "Owner001!",
		DefaultCounter:  "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:     "tech",
		DefaultTechPwd:  "Tech001!",
	}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate failed: %v", err)
	}
	if _, err := db.DB.Exec("INSERT INTO customers (name, phone) VALUES ('x','0900000000')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (1,'AB-123','X')"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /work-orders/{id}/photos", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ListWorkOrderPhotos(w, r, userDummy{}) }))
	mux.HandleFunc("POST /work-orders/{id}/photos", http.HandlerFunc(UploadWorkOrderPhoto))
	mux.HandleFunc("GET /work-orders/{id}/photos/{photo_id}/delete", http.HandlerFunc(DeleteWorkOrderPhoto))
	mux.HandleFunc("GET /photos/{filename}", http.HandlerFunc(ServePhoto))
	return mux, 1
}

func uploadPhoto(t *testing.T, srvURL string) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("--BOUNDARY123\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"photo\"; filename=\"photo.png\"\r\n")
	buf.WriteString("Content-Type: image/png\r\n\r\n")
	buf.Write(validPNG)
	buf.WriteString("\r\n--BOUNDARY123--\r\n")

	resp, err := http.Post(srvURL+"/work-orders/1/photos", "multipart/form-data; boundary=BOUNDARY123", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("upload status = %d, want 302", resp.StatusCode)
	}
}

func TestUploadWorkOrderPhoto(t *testing.T) {
	mux, woID := newTestServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var buf bytes.Buffer
	buf.WriteString("--BOUNDARY123\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"photo\"; filename=\"photo.png\"\r\n")
	buf.WriteString("Content-Type: image/png\r\n\r\n")
	buf.Write(validPNG)
	buf.WriteString("\r\n--BOUNDARY123--\r\n")

	resp, err := http.Post(srv.URL+"/work-orders/1/photos", "multipart/form-data; boundary=BOUNDARY123", bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("upload status = %d, want 302", resp.StatusCode)
	}

	var n int
	if err := db.DB.QueryRow("SELECT COUNT(1) FROM work_order_photos WHERE work_order_id=?", woID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("photo rows = %d, want 1", n)
	}
	var filename string
	if err := db.DB.QueryRow("SELECT filename FROM work_order_photos WHERE work_order_id=?", woID).Scan(&filename); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(photoDir, filename)); err != nil {
		t.Fatalf("photo file not found: %v", err)
	}
}

func TestServePhoto(t *testing.T) {
	mux, _ := newTestServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uploadPhoto(t, srv.URL)

	resp, err := http.Get(srv.URL + "/photos/x.png")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("serve status = %d, want 200", resp.StatusCode)
	}
}

func TestDeleteWorkOrderPhoto(t *testing.T) {
	mux, woID := newTestServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uploadPhoto(t, srv.URL)

	resp, err := http.Get(srv.URL + "/work-orders/1/photos/1/delete")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("delete status = %d, want 302", resp.StatusCode)
	}
	var n int
	if err := db.DB.QueryRow("SELECT COUNT(1) FROM work_order_photos WHERE work_order_id=?", woID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("photo rows after delete = %d, want 0", n)
	}
}

func TestListWorkOrderPhotos(t *testing.T) {
	mux, woID := newTestServer(t)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uploadPhoto(t, srv.URL)

	resp, err := http.Get(srv.URL + "/work-orders/1/photos")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
}
