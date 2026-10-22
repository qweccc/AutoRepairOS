package pages

// AppName 是全站標題（header 與 <title>）的單一來源。
//
// 過去每頁 Layout 都寫死 "AutoRepair OS"，導致 .env 的 APP_NAME 改了沒生效。
// 現在改為啟動時由 main 依 config.AppName 設定一次，所有頁面共用這一個值。
// 空值時維持預設，避免標題變空白。
var AppName = "AutoRepair OS"
