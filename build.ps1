param([string]$Go='D:/DevTools/linshare-go/go/bin/go.exe',[string]$Sdk='D:/DevTools/LongRide-Android/sdk',[string]$Jdk='D:/DevTools/LongRide-Android/jdk/jdk-21.0.12.1+1',[switch]$PackageOnly)
$ErrorActionPreference='Stop'
$root=$PSScriptRoot
$out=Join-Path $root 'out'
$env:JAVA_HOME=$Jdk
$env:PATH="$Jdk/bin;$env:PATH"
function Check-Exit {if($LASTEXITCODE -ne 0){throw "Command failed: $LASTEXITCODE"}}
New-Item -ItemType Directory -Force "$out/classes","$out/dex","$out/lib/arm64-v8a" | Out-Null
Push-Location $root
try {
 if(!$PackageOnly) {
 & $Go test ./...; Check-Exit
 & $Go run github.com/akavel/rsrc@v0.10.2 -ico windows/icon.ico -manifest windows/app.manifest -o desktop_windows_amd64.syso; Check-Exit
 $env:CGO_ENABLED='0';$env:GOOS='windows';$env:GOARCH='amd64'
 & $Go build -trimpath -ldflags '-s -w -H windowsgui' -o "$out/邻享-Go.exe" .; Check-Exit
 $env:GOOS='android';$env:GOARCH='arm64'
 & $Go build -trimpath -ldflags '-s -w' -o "$out/lib/arm64-v8a/libcore.so" .; Check-Exit
 }
 $tools="$Sdk/build-tools/36.0.0";$platform="$Sdk/platforms/android-36/android.jar"
 & "$tools/aapt2.exe" compile --dir android/res -o out/resources.zip; Check-Exit
 & "$tools/aapt2.exe" link -o out/base.apk -I $platform --manifest android/AndroidManifest.xml out/resources.zip; Check-Exit
 $javaFiles=@(Get-ChildItem "$root/android/src" -Filter '*.java' -Recurse | ForEach-Object FullName)
 & "$Jdk/bin/javac.exe" -encoding UTF-8 -source 8 -target 8 -classpath $platform -d "$out/classes" @javaFiles; Check-Exit
 $classes=@(Get-ChildItem "$out/classes" -Filter '*.class' -Recurse | ForEach-Object FullName)
 & "$tools/d8.bat" --lib $platform --min-api 29 --output "$out/dex" @classes; Check-Exit
 Copy-Item -LiteralPath "$out/base.apk" -Destination "$out/unsigned.apk" -Force
 Add-Type -AssemblyName System.IO.Compression.FileSystem
 $zip=[IO.Compression.ZipFile]::Open("$out/unsigned.apk",'Update')
 try {
  [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip,"$out/dex/classes.dex",'classes.dex') | Out-Null
  [IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip,"$out/lib/arm64-v8a/libcore.so",'lib/arm64-v8a/libcore.so') | Out-Null
 } finally {$zip.Dispose()}
 & "$tools/zipalign.exe" -f 4 out/unsigned.apk out/aligned.apk; Check-Exit
 $key=Join-Path $env:USERPROFILE '.android/linshare-go.jks'
 if(!(Test-Path -LiteralPath $key)){& "$Jdk/bin/keytool.exe" -genkeypair -keystore $key -alias linshare -storepass android -keypass android -dname 'CN=LinShare Local Development' -keyalg RSA -validity 10000; Check-Exit}
 & "$tools/apksigner.bat" sign --ks $key --ks-key-alias linshare --ks-pass pass:android --key-pass pass:android --out "$out/邻享-Go.apk" "$out/aligned.apk"; Check-Exit
 Write-Output 'BUILD COMPLETE: out/邻享-Go.exe and out/邻享-Go.apk'
} finally {Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED -ErrorAction SilentlyContinue;Pop-Location}
