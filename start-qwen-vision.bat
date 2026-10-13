@echo off
REM ============================================================
REM  AutoRepairOS 視覺辨識服務（行照/車牌 OCR）
REM  模型：Qwen2.5-VL-3B-Instruct（GGUF Q4_K_M，約 2.5GB RAM/VRAM）
REM  Port：8092（8080=本機 llama、8090=AutoRepairOS 正式服務、8092=本視覺模型）
REM  停止：Ctrl+C 或關閉視窗
REM
REM  注意：AutoRepairOS 預設 VISION_AUTOSTART=true，會自動管理這個服務，
REM        通常不需要手動跑這個 bat。手動跑適合「想讓服務常駐」的情況。
REM ============================================================
cd /d %~dp0

REM 若 llama-server 不在 PATH，請改成完整路徑，例如：
REM set LLAMA=C:\llama.cpp\llama-server.exe
set LLAMA=llama-server

REM 離線優先：優先使用 HuggingFace 快取裡已下載的 GGUF。
REM 用 -hf 規格開機時會連 HuggingFace 驗證，防火牆/離線環境會卡住不動。
set CACHE=%USERPROFILE%\.cache\huggingface\hub\models--unsloth--Qwen2.5-VL-3B-Instruct-GGUF\snapshots
set MODEL=
set MMPROJ=
for /f "delims=" %%F in ('dir /b /s "%CACHE%\Qwen2.5-VL-3B-Instruct-Q4_K_M.gguf" 2^>nul') do set "MODEL=%%F"
for /f "delims=" %%F in ('dir /b /s "%CACHE%\mmproj-BF16.gguf" 2^>nul') do set "MMPROJ=%%F"

if defined MODEL (
    echo 使用本機已下載的模型（離線，不連 HuggingFace）
    %LLAMA% -m "%MODEL%" --mmproj "%MMPROJ%" --port 8092 --host 127.0.0.1
) else (
    echo 本機找不到已下載的模型，改用 -hf 連線下載（防火牆或離線環境可能卡住）
    %LLAMA% -hf unsloth/Qwen2.5-VL-3B-Instruct-GGUF:Q4_K_M --port 8092 --host 127.0.0.1
)

echo.
echo 服務已停止。
pause
