package config

import (
	"os"
	"path/filepath"
	"testing"
)

// makeGGUF 建一個假的 .gguf 檔（內容不重要，只測試路徑解析）。
func makeGGUF(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveVisionGGUF 涵蓋三種寬容寫法 + 主模型不得吃 mmproj。
func TestResolveVisionGGUF(t *testing.T) {
	dir := t.TempDir()
	// 模擬 HF hub 快取：snapshots/<隨機 hash>/ 放主模型與投影檔。
	snap := filepath.Join(dir, "snapshots", "20ce6489")
	makeGGUF(t, filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"))
	makeGGUF(t, filepath.Join(snap, "mmproj-BF16.gguf"))

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"空值", "", ""},
		{"完整路徑", filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"), filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf")},
		{"不存在的路徑原樣回傳", filepath.Join(snap, "nope.gguf"), filepath.Join(snap, "nope.gguf")},
		{"snapshot hash 用萬用字元", filepath.Join(dir, "snapshots", "*", "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"), filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf")},
		{"snapshot 目錄（自動挑主模型）", snap, filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf")},
		// excludePrefix="mmproj" 的用途是主模型不能吃到投影檔：
		// 只符合 mmproj 的 glob 全部被排除 → 回原值（讓上層報錯，而不是靜默載錯模型）。
		{"glob 只符合 mmproj 時全部排除", filepath.Join(snap, "mmproj-*.gguf"), filepath.Join(snap, "mmproj-*.gguf")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveVisionGGUF(tc.in, "mmproj")
			if got != tc.want {
				t.Fatalf("resolveVisionGGUF(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// 整個快取目錄用 */*.gguf 展開時，必須挑到主模型而不是 mmproj。
	got := resolveVisionGGUF(filepath.Join(dir, "snapshots", "*", "*.gguf"), "mmproj")
	if got != filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf") {
		t.Fatalf("glob 展開應挑主模型，got %q", got)
	}
}

// TestResolveVisionMmproj 驗證「只填主模型」時自動補出投影檔。
func TestResolveVisionMmproj(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snapshots", "abc123")
	model := filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf")
	makeGGUF(t, model)
	makeGGUF(t, filepath.Join(snap, "mmproj-BF16.gguf"))

	if got := resolveVisionMmproj("", model); got != filepath.Join(snap, "mmproj-BF16.gguf") {
		t.Fatalf("未填 mmproj 應自動在主模型目錄找到，got %q", got)
	}
	// 明確填了就照填的走（不再自動找）。
	if got := resolveVisionMmproj(model, model); got != model {
		t.Fatalf("明確填 mmproj 應原樣使用，got %q", got)
	}
	// 主模型與 mmproj 都不存在 → 空字串（不帶 --mmproj，維持舊行為）。
	if got := resolveVisionMmproj("", ""); got != "" {
		t.Fatalf("都沒有應回空字串，got %q", got)
	}
	// 目錄裡只有主模型沒有投影檔 → 空字串。
	solo := filepath.Join(dir, "solo")
	makeGGUF(t, filepath.Join(solo, "model.gguf"))
	if got := resolveVisionMmproj("", filepath.Join(solo, "model.gguf")); got != "" {
		t.Fatalf("無投影檔應回空字串，got %q", got)
	}
}

// TestLoadVisionPaths .env 只寫 snapshot 目錄（不含 hash）也能正確解析。
func TestLoadVisionPaths(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "models--unsloth--Qwen2.5-VL-3B-Instruct-GGUF", "snapshots", "deadbeef")
	makeGGUF(t, filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf"))
	makeGGUF(t, filepath.Join(snap, "mmproj-BF16.gguf"))

	// 寫一份 .env 到暫存目錄，讓 loadDotEnv 讀得到（CWD 候選路徑）。
	env := filepath.Join(dir, ".env")
	content := "VISION_MODEL_PATH=" + snap + "\n"
	if err := os.WriteFile(env, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"VISION_MODEL_PATH", "VISION_MMPROJ_PATH"} {
		os.Unsetenv(k)
	}

	cfg := Load()
	if cfg.VisionModelPath != filepath.Join(snap, "Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf") {
		t.Fatalf("ModelPath = %q", cfg.VisionModelPath)
	}
	if cfg.VisionMmprojPath != filepath.Join(snap, "mmproj-BF16.gguf") {
		t.Fatalf("MmprojPath = %q（未填也應自動帶出）", cfg.VisionMmprojPath)
	}
}
