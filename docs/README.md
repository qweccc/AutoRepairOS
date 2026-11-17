# 文件索引

本專案文件分三類，各自獨立維護：

| 分類 | 內容 | 對象 |
|---|---|---|
| [SRS（需求規格）](srs/SRS.md) | 功能需求、術語、規格條目與狀態 | 需求討論、驗收對照 |
| [使用手冊](user-guide/USER_GUIDE_zh-TW.md) | 安裝、日常操作、備份還原、常見問題 | 車廠人員 |
| [開發手冊](development/DEVELOPMENT.md) | 技術棧、目錄、CRUD 標準流程、CSS 設計系統、測試規範 | 開發者／協作 agent |

開發相關的其他文件也放在 `development/`：

- [ROADMAP.md](development/ROADMAP.md) — 路線圖與進度
- [ROUTES_MIGRATION.md](development/ROUTES_MIGRATION.md) — 路由遷移階段紀錄
- [THERMO_NUCLEAR_REMEDIATION.md](development/THERMO_NUCLEAR_REMEDIATION.md) — 程式品質改善計畫

維護規則：

- 新增需求文件或功能規格 → `srs/`
- 使用者看得到的操作說明 → `user-guide/`（未完成的功能不要寫進去）
- 開發規範、流程、計畫 → `development/`；新的開發主題檔案先考慮併入 DEVELOPMENT.md 對應章節，不要另開檔
