#ifndef MyAppVersion
  #define MyAppVersion "0.4.0-dev"
#endif
#ifndef SourceDir
  #error SourceDir must be provided
#endif
#ifndef PackageOutputDir
  #error PackageOutputDir must be provided
#endif

#define MyAppName "ACBH"
#define MyAppPublisher "ACBH"
#define MyAppURL "https://github.com/Ruichen-0079/ACBH"
#define ServiceName "ACBHAgent"

[Setup]
AppId={{A5A72A3D-E735-49FD-80B5-9B89884C9F85}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
DefaultDirName={autopf}\ACBH
DefaultGroupName=ACBH
DisableProgramGroupPage=yes
OutputDir={#PackageOutputDir}
OutputBaseFilename=ACBH-{#MyAppVersion}-windows-x64-setup
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes
RestartApplications=no
UninstallDisplayIcon={app}\acbh-launcher.exe
SetupLogging=yes

[Dirs]
Name: "{commonappdata}\ACBH"; Permissions: system-full admins-full; Flags: uninsneveruninstall
Name: "{commonappdata}\ACBH\logs"; Permissions: system-full admins-full users-readexec; Flags: uninsneveruninstall

[Files]
Source: "{#SourceDir}\acbh-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\acbh-launcher.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\frpc.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\manifest.json"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\THIRD_PARTY_NOTICES.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#SourceDir}\docs\*"; DestDir: "{app}\docs"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\ACBH 公网中转"; Filename: "{app}\acbh-launcher.exe"
Name: "{autodesktop}\ACBH 公网中转"; Filename: "{app}\acbh-launcher.exe"; Tasks: desktopicon

[Registry]
Root: HKCR; Subkey: "acbh"; ValueType: string; ValueData: "URL:ACBH Protocol"; Flags: uninsdeletekey
Root: HKCR; Subkey: "acbh"; ValueName: "URL Protocol"; ValueType: string; ValueData: ""
Root: HKCR; Subkey: "acbh\shell\open\command"; ValueType: string; ValueData: """{app}\acbh-launcher.exe"" ""%1"""

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："; Flags: unchecked

[Run]
Filename: "{sys}\icacls.exe"; Parameters: """{commonappdata}\ACBH"" /inheritance:r /grant:r *S-1-5-18:(OI)(CI)F *S-1-5-32-544:(OI)(CI)F *S-1-5-32-545:(RX)"; Flags: runhidden waituntilterminated
Filename: "{sys}\icacls.exe"; Parameters: """{commonappdata}\ACBH\logs"" /inheritance:r /grant:r *S-1-5-18:(OI)(CI)F *S-1-5-32-544:(OI)(CI)F *S-1-5-32-545:(OI)(CI)RX"; Flags: runhidden waituntilterminated
Filename: "{app}\acbh-agent.exe"; Parameters: "hobby install-service --address ""127.0.0.1:6130"" --frpc ""{app}\frpc.exe"" --app-data-dir ""{commonappdata}\ACBH"""; Flags: runhidden waituntilterminated
Filename: "{sys}\sc.exe"; Parameters: "start {#ServiceName}"; Flags: runhidden waituntilterminated
Filename: "{app}\acbh-launcher.exe"; Description: "打开 ACBH 公网中转"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\net.exe"; Parameters: "stop {#ServiceName} /y"; Flags: runhidden waituntilterminated; RunOnceId: "StopACBHAgent"
Filename: "{app}\acbh-agent.exe"; Parameters: "hobby remove-service"; Flags: runhidden waituntilterminated; RunOnceId: "DeleteACBHAgent"

[Code]
function ServiceExists: Boolean;
var
  ResultCode: Integer;
begin
  Result := Exec(ExpandConstant('{sys}\sc.exe'), 'query {#ServiceName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  Result := '';
  if ServiceExists then
  begin
    Exec(ExpandConstant('{sys}\net.exe'), 'stop {#ServiceName} /y', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;
