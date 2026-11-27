param(
  [string]$Server = $env:RUNWAY_AGENT_SERVER,
  [string]$Token = $env:RUNWAY_AGENT_TOKEN,
  [string]$Runtime = $(if ($env:RUNWAY_AGENT_RUNTIME) { $env:RUNWAY_AGENT_RUNTIME } else { "native" }),
  [string]$Concurrency = $(if ($env:RUNWAY_AGENT_CONCURRENCY) { $env:RUNWAY_AGENT_CONCURRENCY } else { "2" }),
  [string]$Labels = $(if ($env:RUNWAY_AGENT_LABELS) { $env:RUNWAY_AGENT_LABELS } else { "" }),
  [string]$Version = $(if ($env:RUNWAY_AGENT_VERSION) { $env:RUNWAY_AGENT_VERSION } else { "latest" }),
  [string]$Repo = $(if ($env:RUNWAY_AGENT_UPDATE_REPO) { $env:RUNWAY_AGENT_UPDATE_REPO } else { "cubetiqlabs/runway" }),
  [switch]$NoAutoUpdate,
  [switch]$Uninstall
)

$ErrorActionPreference = "Stop"

$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
  [Security.Principal.WindowsBuiltInRole]::Administrator
)
$installDir = if ($isAdmin) { Join-Path $env:ProgramFiles "RunwayAgent" } else { Join-Path $env:LOCALAPPDATA "RunwayAgent" }
$dataDir = if ($isAdmin) { Join-Path $env:ProgramData "RunwayAgent" } else { Join-Path $env:LOCALAPPDATA "RunwayAgent\data" }
$exePath = Join-Path $installDir "runway-agent.exe"
$envFile = Join-Path $dataDir "agent.env"
$taskName = "RunwayAgent"

if ($Uninstall) {
  if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
  }
  Remove-Item -Path $exePath -Force -ErrorAction SilentlyContinue
  Remove-Item -Path $envFile -Force -ErrorAction SilentlyContinue
  Write-Host "[runway-agent] Uninstalled cleanly."
  exit 0
}

if ([string]::IsNullOrWhiteSpace($Server)) { throw "RUNWAY_AGENT_SERVER or -Server is required." }
if ([string]::IsNullOrWhiteSpace($Token)) { throw "RUNWAY_AGENT_TOKEN or -Token is required." }

$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "Arm64" { "arm64" }
  default { "amd64" }
}

$releaseApi = if ($Version -eq "latest") {
  "https://api.github.com/repos/$Repo/releases/latest"
} else {
  if (-not $Version.StartsWith("v")) { $Version = "v$Version" }
  "https://api.github.com/repos/$Repo/releases/tags/$Version"
}

Write-Host "[runway-agent] Fetching release metadata from $Repo ($Version)..."
$rel = Invoke-RestMethod -Uri $releaseApi -Headers @{ "Accept" = "application/vnd.github+json"; "User-Agent" = "runway-agent-installer" }
$tag = $rel.tag_name
$verNoV = $tag.TrimStart("v")
$assetName = "runway-agent_${verNoV}_windows_${arch}.zip"

$asset = $rel.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
$checksumAsset = $rel.assets | Where-Object { $_.name -eq "checksums.txt" } | Select-Object -First 1
if (-not $asset) { throw "Asset $assetName not found in release $tag." }
if (-not $checksumAsset) { throw "checksums.txt not found in release $tag." }

$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("runway-agent-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

try {
  $zipPath = Join-Path $tmpDir $assetName
  $checksumsPath = Join-Path $tmpDir "checksums.txt"

  Write-Host "[runway-agent] Downloading $assetName..."
  Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath -UseBasicParsing
  Invoke-WebRequest -Uri $checksumAsset.browser_download_url -OutFile $checksumsPath -UseBasicParsing

  $expectedLine = Get-Content $checksumsPath | Where-Object { $_ -match "\s+\*?$([Regex]::Escape($assetName))$" } | Select-Object -First 1
  if (-not $expectedLine) { throw "Checksum entry missing for $assetName." }
  $expectedHash = ($expectedLine -split "\s+")[0].ToLowerInvariant()
  $actualHash = (Get-FileHash -Path $zipPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actualHash -ne $expectedHash) {
    throw "SHA-256 verification failed ($actualHash != $expectedHash)."
  }

  New-Item -ItemType Directory -Path $installDir -Force | Out-Null
  New-Item -ItemType Directory -Path $dataDir -Force | Out-Null
  Expand-Archive -Path $zipPath -DestinationPath $tmpDir -Force

  if (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
  }
  Copy-Item -Path (Join-Path $tmpDir "runway-agent.exe") -Destination $exePath -Force

  $autoUpdateVal = if ($NoAutoUpdate) { "false" } else { "true" }
  $envContent = @"
RUNWAY_AGENT_SERVER=$Server
RUNWAY_AGENT_TOKEN=$Token
RUNWAY_AGENT_RUNTIME=$Runtime
RUNWAY_AGENT_WORK_DIR=$dataDir
RUNWAY_AGENT_CONCURRENCY=$Concurrency
RUNWAY_AGENT_LABELS=$Labels
RUNWAY_AGENT_AUTO_UPDATE=$autoUpdateVal
RUNWAY_AGENT_UPDATE_REPO=$Repo
"@
  Set-Content -Path $envFile -Value $envContent -Encoding ASCII

  $runnerWrapper = "while (`$true) { & '$exePath' run --config '$envFile'; Start-Sleep -Seconds 5 }"
  $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -WindowStyle Hidden -Command `"$runnerWrapper`""
  $trigger = if ($isAdmin) { New-ScheduledTaskTrigger -AtStartup } else { New-ScheduledTaskTrigger -AtLogOn }
  $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
  $principal = if ($isAdmin) { New-ScheduledTaskPrincipal -UserId "SYSTEM" -RunLevel Highest } else { New-ScheduledTaskPrincipal -UserId $env:USERNAME }

  Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Force | Out-Null
  Start-ScheduledTask -TaskName $taskName
  Write-Host "[runway-agent] Installed $tag to $exePath and started Scheduled Task '$taskName'."
} finally {
  Remove-Item -Path $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
