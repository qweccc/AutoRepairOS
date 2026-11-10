package config

// 視覺 GGUF 路徑解析：HuggingFace 快取的 snapshot 目錄名是 commit hash，
// **每台機器都不一樣**（重新下載、換版本、換帳號都會變），所以 .env 裡
// 硬寫完整路徑無法跨機器複製。這裡允許三種寬容寫法，自動解析成實際檔案：
//
//  1. 完整檔案路徑（檔案存在）→ 原樣使用
//  2. 含萬用字元（* ? [ ]）    → filepath.Glob 展開，取第一個符合
//  3. 目錄                     → 在該目錄找 *.gguf（主模型排除 mmproj*）
//
// 另外 mmproj 投影檔**可以不設**：沒設時自動在主模型所在目錄找 mmproj*.gguf
// （HF hub 的 snapshot 目錄裡主模型與投影檔一定放在一起）。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// visionGGUFExists 判斷路徑是否為存在的檔案。
func visionGGUFExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// visionIsGlob 判斷字串是否含 glob 萬用字元。
func visionIsGlob(p string) bool {
	return strings.ContainsAny(p, "*?[")
}

// resolveVisionGGUF 把使用者設定的路徑正規化成實際 .gguf 檔案。
//
// excludePrefix 有值時（主模型用 "mmproj"）會把檔名以它開頭的檔案排除，
// 避免把投影檔當主模型載入。全部解析失敗時回傳原值（讓 llama-server
// 啟動時報清楚的「檔案不存在」，而不是靜默回退到會連網的 -hf 模式）。
func resolveVisionGGUF(raw, excludePrefix string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// 1) 含萬用字元：展開後取第一個符合（排除投影檔、排除目錄）。
	if visionIsGlob(raw) {
		matches, _ := filepath.Glob(raw)
		for _, m := range visionSorted(matches) {
			if excludePrefix != "" && strings.HasPrefix(strings.ToLower(filepath.Base(m)), excludePrefix) {
				continue
			}
			if visionGGUFExists(m) {
				return m
			}
		}
		return raw
	}

	// 2) 目錄：在裡面找 *.gguf（主模型取最大的那個，通常是唯一的主模型）。
	if fi, err := os.Stat(raw); err == nil && fi.IsDir() {
		if m := visionPickInDir(raw, excludePrefix); m != "" {
			return m
		}
		return raw
	}

	// 3) 完整檔案路徑：原樣使用（不存在也不改寫，讓上層報錯）。
	return raw
}

// visionPickInDir 在目錄裡挑 *.gguf；excludePrefix 有值時排除該前綴的檔名。
// 有多個符合時取檔案最大的（HF 快取同一目錄不會放兩種量化）。
func visionPickInDir(dir, excludePrefix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestSize int64 = -1
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".gguf") {
			continue
		}
		if excludePrefix != "" && strings.HasPrefix(strings.ToLower(e.Name()), excludePrefix) {
			continue
		}
		// os.Stat 而非 e.Info()：Info() 是 lstat 語意，broken symlink 也回成功
		// （size 是連結目標字串的長度），會被當成最大的 "模型" 選中，
		// 接著 llama-server 就找不到檔案。Stat 會跟著連結，目標不存在就回錯。
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = filepath.Join(dir, e.Name()), info.Size()
		}
	}
	return best
}

// visionSorted 排序 glob 結果，讓「取第一個」是穩定的（同名優先，再看字串序）。
func visionSorted(paths []string) []string {
	out := append([]string(nil), paths...)
	sort.Strings(out)
	return out
}

// visionMmprojInDir 在目錄裡找投影檔：副檔名 .gguf 且檔名含 mmproj（不分大小寫）。
// 有多個時取字母序第一個（HF 快取同一 snapshot 只會有一個投影檔）。
func visionMmprojInDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries { // os.ReadDir 已排序，取第一個符合即穩定
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".gguf") {
			continue
		}
		if strings.Contains(strings.ToLower(e.Name()), "mmproj") {
			// 同上：用 Stat 確認連結目標真的存在，broken symlink 不能回傳
			// （否則 llama-server 拿到不存在的 --mmproj 路徑直接失敗）。
			p := filepath.Join(dir, e.Name())
			if _, err := os.Stat(p); err != nil {
				continue
			}
			return p
		}
	}
	return ""
}

// resolveVisionMmproj 解析 mmproj 投影檔。
//
// mmprojPath 有值就走 resolveVisionGGUF；**沒值時自動在主模型所在目錄找
// mmproj*.gguf** —— 因為 HF hub 的 snapshot 目錄裡兩者一定並存，
// 但 snapshot 的 hash 目錄名每台不同，要求使用者填兩次相同 hash 很容易漏填
// 或填錯。找不到就回空字串（維持舊行為：不帶 --mmproj）。
func resolveVisionMmproj(mmprojPath, modelPath string) string {
	if p := resolveVisionGGUF(mmprojPath, ""); p != "" {
		return p
	}
	if modelPath == "" {
		return ""
	}
	dir := modelPath
	if fi, err := os.Stat(modelPath); err == nil && !fi.IsDir() {
		dir = filepath.Dir(modelPath)
	}
	return visionMmprojInDir(dir)
}
