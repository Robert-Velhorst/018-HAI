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
#if GetEnv("HAI_INSTALLER_PRODUCTION_SIGNING") == "1"
  #if GetEnv("HAI_INSTALLER_SIGNED_UNINSTALLER_DIR") == ""
    #error Production signing requires a per-build signed-uninstaller directory
  #endif
SignTool=hai
SignedUninstaller=yes
SignedUninstallerDir={#GetEnv("HAI_INSTALLER_SIGNED_UNINSTALLER_DIR")}
#else
SignedUninstaller=no
#endif

[Files]
Source: "..\release\payload\installer\windows\Run-HAI-OpenClawMaintenance.ps1"; DestDir: "{app}\app\installer\windows"; Flags: ignoreversion
Source: "..\release\payload\*"; Excludes: "\installer\windows\hai-openclaw-maintenance.exe"; DestDir: "{app}\app"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\release\payload\installer\windows\hai-openclaw-maintenance.exe"; DestDir: "{app}\app\installer\windows"; DestName: "hai-openclaw-maintenance.pending.exe"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\HAI Local\Start HAI"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Start-HAI.ps1"" -PauseOnError"; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Open local dashboard"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Open-HAI.ps1"" -PauseOnError"; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\HAI status"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\HAI-Status.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\OpenClaw maintenance"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -NoExit -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Run-HAI-OpenClawMaintenance.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Test local agent connector"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Test-HAI-LocalConnector.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Stop HAI"; Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Stop-HAI.ps1"""; WorkingDir: "{app}\app"
Name: "{autoprograms}\HAI Local\Uninstall HAI"; Filename: "{uninstallexe}"

[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\app\installer\windows\Start-HAI.ps1"" -PauseOnError"; WorkingDir: "{app}\app"; Description: "Start HAI and open the local dashboard"; Flags: postinstall nowait skipifsilent

[Code]
const
  HaiMaintenanceMutexName = 'Global\HAI.OpenClawMaintenance';
  WaitObject0 = $00000000;
  WaitAbandoned = $00000080;
  WaitTimeout = $00000102;

var
  HaiMaintenanceMutexHandle: THandle;
  HaiMaintenanceMutexOwned: Boolean;

function CreateMutexW(lpMutexAttributes: THandle; bInitialOwner: LongBool; lpName: String): THandle;
  external 'CreateMutexW@kernel32.dll stdcall';
function WaitForSingleObject(hHandle: THandle; dwMilliseconds: LongWord): LongWord;
  external 'WaitForSingleObject@kernel32.dll stdcall';
function ReleaseMutex(hMutex: THandle): LongBool;
  external 'ReleaseMutex@kernel32.dll stdcall';
function CloseHandle(hObject: THandle): LongBool;
  external 'CloseHandle@kernel32.dll stdcall';

function AcquireHaiMaintenanceMutex: Boolean;
var
  WaitResult: LongWord;
begin
  if HaiMaintenanceMutexOwned then
  begin
    Result := True;
    Exit;
  end;

  HaiMaintenanceMutexHandle := CreateMutexW(0, False, HaiMaintenanceMutexName);
  if HaiMaintenanceMutexHandle = 0 then
  begin
    Log('HAI installer: could not create the maintenance synchronization lock.');
    Result := False;
    Exit;
  end;

  WaitResult := WaitForSingleObject(HaiMaintenanceMutexHandle, 0);
  if (WaitResult = WaitObject0) or (WaitResult = WaitAbandoned) then
  begin
    HaiMaintenanceMutexOwned := True;
    Result := True;
    Exit;
  end;

  if WaitResult = WaitTimeout then
    Log('HAI installer: maintenance is active; setup will stop before replacing files.')
  else
    Log('HAI installer: maintenance lock wait failed; setup will stop before replacing files.');
  CloseHandle(HaiMaintenanceMutexHandle);
  HaiMaintenanceMutexHandle := 0;
  Result := False;
end;

function ReleaseHaiMaintenanceMutex: Boolean;
begin
  Result := True;
  if HaiMaintenanceMutexOwned then
  begin
    if not ReleaseMutex(HaiMaintenanceMutexHandle) then
    begin
      Log('HAI installer: releasing the maintenance synchronization lock failed.');
      Result := False;
      Exit;
    end;
    HaiMaintenanceMutexOwned := False;
  end;

  if HaiMaintenanceMutexHandle <> 0 then
  begin
    if CloseHandle(HaiMaintenanceMutexHandle) then
      HaiMaintenanceMutexHandle := 0
    else
      Log('HAI installer: closing the maintenance synchronization handle failed.');
  end;
end;

function HasLegacyMaintenanceWorker: Boolean;
var
  ExitCode: Integer;
  Parameters: String;
begin
  Parameters := '-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command ' +
    '"$ErrorActionPreference=''Stop''; try { $active = @(Get-CimInstance -ClassName Win32_Process | Where-Object { $_.Name -ieq ''hai-openclaw-maintenance.exe'' }); if ($active.Count -gt 0) { exit 23 }; exit 0 } catch { exit 24 }"';
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Parameters, '', SW_HIDE, ewWaitUntilTerminated, ExitCode) then
  begin
    Log('HAI installer: the legacy-worker safety check could not be started.');
    Result := True;
    Exit;
  end;

  if ExitCode = 23 then
  begin
    Log('HAI installer: an older maintenance worker is active; setup will stop before replacing files.');
    Result := True;
  end
  else if ExitCode <> 0 then
  begin
    Log('HAI installer: the legacy-worker safety check failed; setup will stop before replacing files.');
    Result := True;
  end
  else
    Result := False;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if not AcquireHaiMaintenanceMutex then
  begin
    Result := 'HAI maintenance or another HAI upgrade is active. No files were replaced. Wait for it to finish, then retry setup.';
    Exit;
  end;

  if HasLegacyMaintenanceWorker then
    Result := 'An older HAI maintenance worker is running or could not be checked. No files were replaced. Wait for it to finish, then retry setup.';
end;

function RunHaiMaintenanceTaskManager(const Action: String; SkipImmediateRun: Boolean; var ExitCode: Integer): Boolean;
var
  Parameters: String;
begin
  Parameters := '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' +
    AddQuotes(ExpandConstant('{app}\app\installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1')) +
    ' -Action ' + Action;
  if SkipImmediateRun then
    Parameters := Parameters + ' -SkipImmediateRun';
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Parameters,
    ExpandConstant('{app}\app'), SW_HIDE, ewWaitUntilTerminated, ExitCode);
end;

function RestoreHaiMaintenanceTaskAfterCancelledUninstall(var ExitCode: Integer): Boolean;
var
  Parameters: String;
begin
  Parameters := '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' +
    AddQuotes(ExpandConstant('{app}\app\installer\windows\Manage-HAI-OpenClawMaintenanceTask.ps1')) +
    ' -Action Register -SkipImmediateRun -RestoreAfterCancelledUninstall';
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Parameters,
    ExpandConstant('{app}\app'), SW_HIDE, ewWaitUntilTerminated, ExitCode);
end;

function StopHaiRuntimeForUninstall(var ExitCode: Integer): Boolean;
var
  Parameters: String;
begin
  Parameters := '-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File ' +
    AddQuotes(ExpandConstant('{app}\app\installer\windows\Hai-InstallerSupport.ps1')) +
    ' -StopRuntimeForUninstall';
  Result := Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'), Parameters,
    ExpandConstant('{app}\app'), SW_HIDE, ewWaitUntilTerminated, ExitCode);
end;

function InitializeUninstall(): Boolean;
var
  ExitCode: Integer;
  MaintenanceRestored: Boolean;
begin
  Result := False;
  ExitCode := -1;
  if not RunHaiMaintenanceTaskManager('Unregister', False, ExitCode) or (ExitCode <> 0) then
  begin
    Log('HAI uninstaller: the HAI-owned maintenance task could not be safely removed; uninstall was cancelled.');
    if not UninstallSilent then
      MsgBox('HAI could not safely remove its scheduled maintenance task. Uninstall was cancelled and your HAI files and data were preserved. Review the setup log, then retry.', mbError, MB_OK);
    Exit;
  end;

  if not AcquireHaiMaintenanceMutex then
  begin
    ExitCode := -1;
    MaintenanceRestored := RestoreHaiMaintenanceTaskAfterCancelledUninstall(ExitCode) and (ExitCode = 0);
    if MaintenanceRestored then
      Log('HAI uninstaller: maintenance became active; uninstall was cancelled and the scheduled task was restored.')
    else
      Log('HAI uninstaller: maintenance became active; uninstall was cancelled but the scheduled task could not be restored.');
    if not UninstallSilent then
    begin
      if MaintenanceRestored then
        MsgBox('HAI maintenance became active before uninstall could safely remove its files. Uninstall was cancelled, your HAI data was preserved, and scheduled maintenance was restored. Retry after maintenance finishes.', mbError, MB_OK)
      else
        MsgBox('HAI maintenance became active before uninstall could safely remove its files. Uninstall was cancelled and your HAI data was preserved, but scheduled maintenance could not be restored. Review the setup log and retry after maintenance finishes.', mbError, MB_OK);
    end;
    Exit;
  end;

  ExitCode := -1;
  if not RunHaiMaintenanceTaskManager('AssertAbsent', False, ExitCode) or (ExitCode <> 0) then
  begin
    Log('HAI uninstaller: a maintenance task reappeared before file removal; uninstall was cancelled.');
    if not UninstallSilent then
      MsgBox('A scheduled HAI maintenance task reappeared while uninstall was starting. Uninstall was cancelled and your HAI files and data were preserved. Retry uninstall.', mbError, MB_OK);
    ReleaseHaiMaintenanceMutex;
    Exit;
  end;

  ExitCode := -1;
  if not StopHaiRuntimeForUninstall(ExitCode) or (ExitCode <> 0) then
  begin
    Log('HAI uninstaller: the HAI Compose runtime could not be verified and stopped safely; uninstall was cancelled. HAI files and data were preserved.');
    ReleaseHaiMaintenanceMutex;
    ExitCode := -1;
    MaintenanceRestored := RunHaiMaintenanceTaskManager('Register', True, ExitCode) and (ExitCode = 0);
    if MaintenanceRestored then
      Log('HAI uninstaller: restored the HAI-owned maintenance task after runtime stop was refused.')
    else
      Log('HAI uninstaller: the maintenance task could not be restored after runtime stop was refused; manual recovery is required.');
    if not UninstallSilent then
    begin
      if MaintenanceRestored then
        MsgBox('HAI could not verify ownership of its Compose runtime and stop it safely. Uninstall was cancelled; HAI files and data were preserved, and scheduled maintenance was restored. Restore the original HAI environment and resolve the runtime ownership issue, then retry.', mbError, MB_OK)
      else
        MsgBox('HAI could not verify ownership of its Compose runtime and stop it safely. Uninstall was cancelled and HAI files and data were preserved, but scheduled maintenance could not be restored. Review the setup log and restore the protected HAI environment before retrying.', mbError, MB_OK);
    end;
    Exit;
  end;
  Result := True;
end;

procedure DeinitializeUninstall();
begin
  if not ReleaseHaiMaintenanceMutex then
    Log('HAI uninstaller: releasing the maintenance synchronization lock failed.');
end;

procedure RunHaiInstallerSupport(const Arguments, FailureMessage, LogLabel: String);
var
  ExitCode: Integer;
begin
  Log('HAI installer: ' + LogLabel + ' started.');
  if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
    '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ' +
    AddQuotes(ExpandConstant('{app}\app\installer\windows\Hai-InstallerSupport.ps1')) + ' ' + Arguments,
    ExpandConstant('{app}\app'), SW_HIDE, ewWaitUntilTerminated, ExitCode) then
  begin
    Log('HAI installer: ' + LogLabel + ' could not be started.');
    RaiseException(FailureMessage);
  end;
  if ExitCode <> 0 then
  begin
    Log('HAI installer: ' + LogLabel + ' failed with PowerShell exit code ' + IntToStr(ExitCode) + '.');
    RaiseException(FailureMessage);
  end;
  Log('HAI installer: ' + LogLabel + ' completed successfully.');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    if not ReleaseHaiMaintenanceMutex then
      RaiseException('HAI could not release its maintenance lock. Setup is stopping safely; retry after it exits.');

    RunHaiInstallerSupport('-PromoteMaintenanceWorker',
      'HAI could not safely install the maintenance worker. The existing worker was preserved; review the setup log and retry.',
      'maintenance worker promotion');

    if WizardSilent then
      RunHaiInstallerSupport('-ConfigureSilentUpgrade',
        'The HAI silent-upgrade migration or task registration failed. Setup is reporting failure so it can be retried safely.',
        'silent-upgrade migration and task registration');

    if WizardSilent then
      RunHaiInstallerSupport('-StartMaintenanceTask',
        'HAI could not start its verified maintenance check after releasing installer synchronization.',
        'post-upgrade maintenance check');
  end;
end;

procedure DeinitializeSetup;
begin
  ReleaseHaiMaintenanceMutex;
end;
