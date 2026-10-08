import re
p="C:/Users/fr/AutoRepairOS/scripts/container_logs.txt"
raw=open(p,"rb").read()
# strip UTF-16 BOM if present
if raw.startswith(b"\xff\xfe"):
    raw=raw[2:]
for enc in ["gb18030","gbk","utf-16"]:
    try:
        txt=raw.decode(enc); break
    except Exception: txt=None
if txt is None:
    print("NO DECODE")
else:
    lines=[l for l in txt.split("\n") if l.strip()]
    filt="排程器|任務|backup|Backup|schedul|Schedul|port|已啟動|已停止|panic|error|Error|FAIL|success|新增|處理|完工|保養|migration|notification|daily|work_order|maintenance|processing|processing %d"
    print("=== SCHEDULER/BACKUP LINES ===")
    for l in lines:
        if l.strip() and re.search(filt, l.strip()):
            print(l.strip())
