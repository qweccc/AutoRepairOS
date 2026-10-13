@echo off
REM ============================================================
REM  AutoRepairOS 視覺辨識服務（行照/車牌 OCR）
REM  模型：Qwen2.5-VL-3B-Instruct（GGUF Q4_K_M，約 2.5GB RAM/VRAM）
REM  Port：8092（8080=正式服務、8081=llama ornith1.5、8092=本視覺模型）
REM  首次執行會自動從 HuggingFace 下載模型 + mmproj（約 2-3GB）
REM  停止：Ctrl+C 或關閉視窗
REM ============================================================
cd /d %~dp0

REM 若 llama-server 不在 PATH，請改成完整路徑，例如：
REM set LLAMA=C:\llama.cpp\llama-server.exe
set LLAMA=llama-server

echo 啟動視覺辨識服務 port 8092 ...
echo 模型：Qwen2.5-VL-3B-Instruct-GGUF:Q4_K_M
echo.

%LLAMA% -hf unsloth/Qwen2.5-VL-3B-Instruct-GGUF:Q4_K_M --port 8092 --host 127.0.0.1

echo.
echo 服務已停止。
pause
