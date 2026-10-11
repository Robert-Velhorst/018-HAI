#define MyAppName "HAI Automation Hub"
#define MyAppVersion GetEnv("HAI_INSTALLER_VERSION")
#if MyAppVersion == ""
  #define MyAppVersion "0.1.0-dev"
#endif
#define MyOutputDir GetEnv("HAI_INSTALLER_OUTPUT_DIR")
#if MyOutputDir == ""
  #define MyOutputDir "..\\release"
#endif

[Setup]
AppId={{2F1FA2B5-68B6-4EAF-A4B4-7E44F456B889}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher=Noodzakelijk Online
DefaultDirName={autopf}\HAI
DefaultGroupName=HAI Local
DisableProgramGroupPage=yes
OutputDir={#MyOutputDir}
OutputBaseFilename=HAI-Setup-{#MyAppVersion}
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
WizardStyle=modern
UninstallDisplayName=HAI Automation Hub

[Files]
Source: "..\release\payload\*"; DestDir: "{app}\app"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\HAI Local\Start HAI"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Start-HAI.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Enable local model"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Enable-LocalModel.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Open local dashboard"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Open-HAI.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\HAI status"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\HAI-Status.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Stop HAI"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Stop-HAI.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Uninstall HAI"; Filename: "{uninstallexe}"

[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Start-HAI.ps1"""; WorkingDir: "{app}\app"; Description: "Start HAI and open the local dashboard"; Flags: postinstall nowait skipifsilent
