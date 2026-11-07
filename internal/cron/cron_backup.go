package cron

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
)

func defaultBackupConfig() BackupConfig {
	dir := strings.TrimSpace(os.Getenv("BACKUP_DIR"))
	if dir == "" {
		dir = "data/backups"
	}
	return BackupConfig{BackupDir: dir, PhotoDir: "data/photos"}
}

// SetBackupDir 覆寫備份輸出目錄。
func SetBackupDir(dir string) {
	if dir != "" {
		backupCfg.BackupDir = dir
	}
}

// DailyBackup 將一致性 SQLite 快照與完整照片目錄封裝成 gzip tar 備份。
func DailyBackup() error {
	// Load .env before resolving BACKUP_DIR so local and container deployments agree.
	config.Load()
	if configured := strings.TrimSpace(os.Getenv("BACKUP_DIR")); configured != "" {
		backupCfg.BackupDir = configured
	}
	if err := os.MkdirAll(backupCfg.BackupDir, 0755); err != nil {
		return recordBackupFailure("", "建立備份目錄失敗: "+err.Error())
	}

	stamp := time.Now().Format("20060102_150405")
	dbDst := filepath.Join(backupCfg.BackupDir, "autorepair_"+stamp+".db")
	archiveDst := filepath.Join(backupCfg.BackupDir, "autorepair_"+stamp+".tar.gz")

	// WAL 模式下直接複製 .db 會漏掉尚未 checkpoint 的資料（-wal/-shm），
	// 必須走 SQLite 線上備份 API 或先做 WAL checkpoint。
	dbSize, err := backupDB(dbDst)
	if err != nil {
		return recordBackupFailure(archiveDst, "備份 DB 失敗: "+err.Error())
	}

	photoCount, photoSize, err := dirStats(backupCfg.PhotoDir)
	if err != nil {
		_ = os.Remove(dbDst)
		return recordBackupFailure(archiveDst, "統計照片目錄失敗: "+err.Error())
	}
	if err := createBackupArchive(dbDst, archiveDst, backupCfg.PhotoDir); err != nil {
		_ = os.Remove(dbDst)
		return recordBackupFailure(archiveDst, "建立照片備份封存失敗: "+err.Error())
	}
	if err := os.Remove(dbDst); err != nil && !os.IsNotExist(err) {
		log.Printf("移除暫存 DB 備份 %s 失敗: %v", dbDst, err)
	}

	// 保留最近 30 天；同一天重跑只保留最新的完整封存。
	pruneBackups(30 * 24 * time.Hour)
	pruneSameDay(stamp[:8], archiveDst)

	info, err := os.Stat(archiveDst)
	if err != nil {
		return recordBackupFailure(archiveDst, "查詢備份封存大小失敗: "+err.Error())
	}
	log.Printf("每日備份完成: %s (%d bytes, DB %d bytes, 照片 %d 張 / %d bytes)",
		archiveDst, info.Size(), dbSize, photoCount, photoSize)
	return recordBackup(archiveDst, dbSize, int64(photoCount), photoSize, "")
}

func recordBackupFailure(path, cause string) error {
	if err := recordBackup(path, 0, 0, 0, cause); err != nil {
		return fmt.Errorf("%s；記錄備份失敗也失敗: %w", cause, err)
	}
	return errors.New(cause)
}

func recordBackup(path string, size int64, photoCount, photoSize int64, errMsg string) error {
	status := "success"
	if errMsg != "" {
		status = "failed"
	}
	_, err := db.DB.Exec(`INSERT INTO backup_records
		(backup_date, db_backup_path, db_size, photo_count, photo_size, status, error_message)
		VALUES (?,?,?,?,?,?,?)`,
		time.Now().Format("2006-01-02"), path, size, photoCount, photoSize, status, errMsg)
	return err
}

// pruneSameDay 刪除同一日期（YYYYMMDD）的舊備份，只保留剛寫入的 keepFile。
func pruneSameDay(day, keepPath string) {
	entries, err := os.ReadDir(backupCfg.BackupDir)
	if err != nil {
		return
	}
	prefix := "autorepair_" + day
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".db") && !strings.HasSuffix(e.Name(), ".tar.gz")) {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if filepath.Join(backupCfg.BackupDir, e.Name()) == keepPath {
			continue
		}
		os.Remove(filepath.Join(backupCfg.BackupDir, e.Name()))
	}
}

func pruneBackups(maxAge time.Duration) {
	entries, err := os.ReadDir(backupCfg.BackupDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".db") && !strings.HasSuffix(e.Name(), ".tar.gz")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(backupCfg.BackupDir, e.Name()))
		}
	}
}

// backupDB 使用 SQLite 線上備份 API（VACUUM INTO），確保 WAL 內的資料一併寫入。
func backupDB(dst string) (int64, error) {
	var size int64

	// VACUUM INTO 會產生完整且一致的快照，含所有已提交的交易。
	if _, err := db.DB.Exec(`VACUUM INTO ?`, dst); err != nil {
		// 某些 SQLite 版本/驅動不支援 VACUUM INTO，退回 checkpoint + 複製。
		db.MustExec("PRAGMA wal_checkpoint(TRUNCATE)")
		n, cerr := copyFile(filepath.Join(backupSrcPath(), "autorepair.db"), dst)
		if cerr != nil {
			return 0, fmt.Errorf("VACUUM INTO 失敗 (%v) 且複製備份也失敗: %v", err, cerr)
		}
		return n, nil
	}

	info, err := os.Stat(dst)
	if err != nil {
		return 0, fmt.Errorf("查詢備份檔大小失敗: %v", err)
	}
	size = info.Size()
	return size, nil
}

// backupSrcPath 從設定取得來源 DB 目錄，移除 file: 與 query string。
func backupSrcPath() string {
	raw := config.Load().DBPath
	raw = strings.TrimPrefix(raw, "file:")
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	if raw == "" {
		return "data"
	}
	return filepath.Dir(raw)
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("開啟來源失敗: %v", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("建立備份檔失敗: %v", err)
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return n, err
	}
	return n, nil
}

func dirStats(dir string) (count int, size int64, err error) {
	rootInfo, statErr := os.Stat(dir)
	if os.IsNotExist(statErr) {
		return 0, 0, nil
	}
	if statErr != nil {
		return 0, 0, statErr
	}
	if !rootInfo.IsDir() {
		return 0, 0, fmt.Errorf("照片來源不是目錄: %s", dir)
	}
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("照片目錄含不支援的檔案類型: %s", path)
		}
		count++
		size += info.Size()
		return nil
	})
	return count, size, err
}

func createBackupArchive(dbPath, archivePath, photoDir string) error {
	out, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	writeErr := func() error {
		if err := addArchiveFile(tw, dbPath, filepath.Base(dbPath)); err != nil {
			return fmt.Errorf("加入 SQLite 快照失敗: %w", err)
		}
		info, err := os.Stat(photoDir)
		if os.IsNotExist(err) {
			if err := tw.WriteHeader(&tar.Header{Name: "photos/", Typeflag: tar.TypeDir, Mode: 0755, ModTime: time.Now()}); err != nil {
				return err
			}
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("照片來源不是目錄: %s", photoDir)
		}
		rootHeader, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rootHeader.Name = "photos/"
		if err := tw.WriteHeader(rootHeader); err != nil {
			return err
		}
		return filepath.WalkDir(photoDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path == photoDir {
				return nil
			}
			rel, err := filepath.Rel(photoDir, path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() && !info.IsDir() {
				return fmt.Errorf("照片目錄含不支援的檔案類型: %s", path)
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = "photos/" + filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
			}
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			return addArchiveContents(tw, path)
		})
	}()
	tarErr := tw.Close()
	gzipErr := gz.Close()
	closeErr := out.Close()
	if writeErr != nil {
		_ = os.Remove(archivePath)
		return writeErr
	}
	if tarErr != nil {
		_ = os.Remove(archivePath)
		return tarErr
	}
	if gzipErr != nil {
		_ = os.Remove(archivePath)
		return gzipErr
	}
	if closeErr != nil {
		_ = os.Remove(archivePath)
		return closeErr
	}
	return nil
}

func addArchiveFile(tw *tar.Writer, path, archiveName string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = archiveName
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	return addArchiveContents(tw, path)
}

func addArchiveContents(tw *tar.Writer, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(tw, in)
	closeErr := in.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// --- 內建任務 ---
