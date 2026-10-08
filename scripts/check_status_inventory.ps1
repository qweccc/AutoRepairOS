$Base="http://localhost:8090"
$states=@("pending","in_progress","waiting_parts","completed","paid","draft","cancelled")
$sep = "&"

function Login {
    param([string]$un,[string]$pw)
    $data="username=$un&password=$pw"
    $bytes=[System.Text.Encoding]::UTF8.GetBytes($data)
    $req=[System.Net.WebRequest]::Create($Base+"/login")
    $req.Method="POST"
    $req.ContentType="application/x-www-form-urlencoded; charset=utf-8"
    $req.ContentBytes=$bytes
    $resp=$req.GetResponse()
    $stream=$resp.GetResponseStream()
    $reader=[System.IO.StreamReader]::new($stream,[System.Text.Encoding]::UTF8)
    $body=$reader.ReadToEnd()
    $reader.Close(); $stream.Close(); $resp.Close()
    return $resp.StatusCode,$body
}

function Get {
    param([string]$path,[string]$cookie)
    $url=$Base+$path
    $req=[System.Net.WebRequest]::Create($url)
    $req.Headers.Add("Cookie","session_token=$cookie")
    $resp=$req.GetResponse()
    $stream=$resp.GetResponseStream()
    $reader=[System.IO.StreamReader]::new($stream,[System.Text.Encoding]::UTF8)
    $body=$reader.ReadToEnd()
    $reader.Close(); $stream.Close(); $resp.Close()
    return $resp.StatusCode,$body
}

$st,$b=Login("owner","Owner001!")
$cookie=""
if($b -match 'session_token=([^;]+)'){ $cookie=$matches[1] }
Write-Output "owner login: $st | cookie: $cookie"

$st,$b=Get("/api/workorders","$cookie")
Write-Output "`nowner /api/workorders: $st | body length: $($b.Length)"

$counts=@{}
$matches=[regex]::Matches($b,'"status"\s*:\s*"([^"]*)"')
foreach($m in $matches){
    $key=$m.Groups[1].Value
    $counts[$key]=(if($counts.ContainsKey($key)){ $counts[$key]+1 }else{ 1 })
}
foreach($k in ($counts.Keys | Sort-Object)){ Write-Output "  all [$k]: $($counts[$k])" }

Write-Output "`n=== 各狀態筆數 ==="
foreach($s in $states){
    $q="/api/workorders?status=$s"+$sep+"limit=100"
    $st,$b=Get($q,"$cookie")
    $n=([regex]::Matches($b,'"id"\s*:\s*\d+')).Count
    Write-Output "  [$s] $n 筆"
}

Write-Output "`n=== 樣本工單 ==="
foreach($s in @("pending","in_progress","waiting_parts","completed")){
    $q="/api/workorders?status=$s"+$sep+"limit=3"
    $st,$b=Get($q,"$cookie")
    $rows=[regex]::Matches($b,'\{"id":(\d+),"status":"$s","total_amount":"([^"]*)","approval_status":"([^"]*)"\}')
    Write-Output "  [$s] $($rows.Count) 筆"
    foreach($row in $rows | Select-Object -First 3){
        Write-Output "     #$($row.Groups[1].Value) | total=$($row.Groups[2].Value) | approval=$($row.Groups[3].Value)"
    }
}

Write-Output "`n=== 權限隔離檢查 ==="
$roleSets=@{ "counter"=@("counter","Counter001!"); "tech"=@("tech","Tech001!") }
foreach($role in @("counter","tech")){
    $ps=$roleSets[$role]
    $st,$b=Login($ps[0],$ps[1])
    Write-Output "  [$role] login: $st"
    $rc=""
    if($b -match 'role\s*:\s*"([^"]*)"'){ $rc=$matches[1] }
    Write-Output "     角色:$rc | 內容長度:$($b.Length)"
    if($rc -eq "tech"){
        $st,$b=Get("/work-orders","$cookie")
        Write-Output "     GET /work-orders: $st | 長度:$($b.Length)"
        $st,$b=Get("/inventory","$cookie")
        Write-Output "     GET /inventory: $st | 長度:$($b.Length)"
    }
}
