Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef VERSION
  !error "VERSION is required"
!endif
!ifndef OUTFILE
  !error "OUTFILE is required"
!endif
!ifndef BINARY
  !error "BINARY is required"
!endif
!ifndef ASSOC_NSH
  !error "ASSOC_NSH is required"
!endif
!ifdef ICON
  !define MUI_ICON "${ICON}"
  !define MUI_UNICON "${ICON}"
!endif

Name "Block"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\Block"
RequestExecutionLevel admin
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "Block"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "Block installer"

Var BLOCK_SCOPE_ID
Var BLOCK_MACHINE_PROGID
Var BLOCK_UNINSTALL_KEY
Var BLOCK_SHORTCUT_PATH
Var BLOCK_DISPLAY_NAME

; Compute deterministic 16-hex-char scope ID from lowercased $INSTDIR (with
; trailing backslashes stripped) using Windows shlwapi::HashData so coexisting
; machine-wide installs (e.g. C:\Block-A and D:\Block-B) always receive unique
; ProgIDs, uninstall entries, shortcuts, and deferred per-user cleanup tasks.
!macro BLOCK_COMPUTE_SCOPE_IDENTITY PREFIX
  Push $0
  Push $1
  Push $2
  Push $3
  Push $4
  Push $5

  StrCpy $0 "$INSTDIR"
  ${PREFIX}_strip_slash:
    StrLen $1 $0
    IntCmp $1 3 ${PREFIX}_slash_done ${PREFIX}_slash_done 0
    StrCpy $2 $0 1 -1
    StrCmp $2 "\" 0 ${PREFIX}_slash_done
    IntOp $1 $1 - 1
    StrCpy $0 $0 $1
    Goto ${PREFIX}_strip_slash
  ${PREFIX}_slash_done:

  System::Call 'user32::CharLowerW(t r0r0)'
  StrLen $1 $0
  IntOp $1 $1 * 2
  System::Call '*(i 0, i 0)p.r4'
  System::Call 'shlwapi::HashData(t r0, i r1, p r4, i 8)i.r5'
  System::Call '*$4(i .r2, i .r3)'
  System::Free $4
  IntFmt $2 "%08x" $2
  IntFmt $3 "%08x" $3
  StrCpy $BLOCK_SCOPE_ID "$2$3"
  StrCpy $BLOCK_MACHINE_PROGID "Block.Machine.$BLOCK_SCOPE_ID"
  StrCpy $BLOCK_UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block-$BLOCK_SCOPE_ID"

  StrCmp $0 "$PROGRAMFILES64\Block" 0 ${PREFIX}_custom_path
    StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block.lnk"
    StrCpy $BLOCK_DISPLAY_NAME "Block"
    Goto ${PREFIX}_id_done
  ${PREFIX}_custom_path:
    StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block-$BLOCK_SCOPE_ID.lnk"
    StrCpy $BLOCK_DISPLAY_NAME "Block ($INSTDIR)"
  ${PREFIX}_id_done:

  Pop $5
  Pop $4
  Pop $3
  Pop $2
  Pop $1
  Pop $0
!macroend

; Reconcile stale HKLM OpenWithProgids values for PROGID during an in-place
; upgrade: remove PROGID from any .ext\OpenWithProgids not present in
; NEW_EXTS_KEY, while preserving extensions that remain selected as a default
; handler in HKLM or any loaded user hive (UserChoice / UserChoiceLatest / Classes).
!macro INST_RECONCILE_HKLM_PROGID PROGID NEW_EXTS_KEY LABEL_PREFIX
  Push $3
  Push $4
  Push $5
  Push $6
  Push $7
  Push $8

  StrCpy $3 0
  ${LABEL_PREFIX}_ext_loop:
    EnumRegKey $4 HKLM "Software\Classes" $3
    StrCmp $4 "" ${LABEL_PREFIX}_done
    IntOp $3 $3 + 1
    StrCpy $5 $4 1
    StrCmp $5 "." 0 ${LABEL_PREFIX}_ext_loop
    ClearErrors
    ReadRegStr $5 HKLM "Software\Classes\$4\OpenWithProgids" "${PROGID}"
    IfErrors ${LABEL_PREFIX}_ext_loop
    ClearErrors
    ReadRegStr $5 HKLM "${NEW_EXTS_KEY}" "$4"
    IfErrors ${LABEL_PREFIX}_check_default ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_check_default:
    ClearErrors
    ReadRegStr $8 HKLM "Software\Classes\$4" ""
    StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
    StrCpy $6 0
    ${LABEL_PREFIX}_hku_loop:
      EnumRegKey $7 HKU "" $6
      StrCmp $7 "" ${LABEL_PREFIX}_remove_stale
      IntOp $6 $6 + 1
      ReadRegStr $8 HKU "$7\Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\$4\UserChoice" "ProgId"
      StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
      ReadRegStr $8 HKU "$7\Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\$4\UserChoiceLatest\ProgId" "ProgId"
      StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
      ReadRegStr $8 HKU "$7\Software\Classes\$4" ""
      StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
      ReadRegStr $8 HKU "$7\$4" ""
      StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
      Goto ${LABEL_PREFIX}_hku_loop
  ${LABEL_PREFIX}_remove_stale:
    DeleteRegValue HKLM "Software\Classes\$4\OpenWithProgids" "${PROGID}"
    Goto ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_done:
    ClearErrors

  Pop $8
  Pop $7
  Pop $6
  Pop $5
  Pop $4
  Pop $3
!macroend

; Clean one HKLM ProgID and its OpenWithProgids entries if its shell\open\command
; points to "$INSTDIR\block.exe" "%1".
!macro UN_CLEAN_HKLM_PROGID PROGID LABEL_PREFIX
  ReadRegStr $2 HKLM "Software\Classes\${PROGID}\shell\open\command" ""
  StrCmp $2 '"$INSTDIR\block.exe" "%1"' 0 ${LABEL_PREFIX}_done
  DeleteRegKey HKLM "Software\Classes\${PROGID}"
  StrCpy $3 0
  ${LABEL_PREFIX}_ext_loop:
    EnumRegKey $4 HKLM "Software\Classes" $3
    StrCmp $4 "" ${LABEL_PREFIX}_done
    IntOp $3 $3 + 1
    StrCpy $5 $4 1
    StrCmp $5 "." 0 ${LABEL_PREFIX}_ext_loop
    DeleteRegValue HKLM "Software\Classes\$4\OpenWithProgids" "${PROGID}"
    Goto ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_done:
!macroend

; Enumerate every Block.* ProgID under HKU\<ROOT_SUBKEY> and remove any ProgID
; (plus its .ext\OpenWithProgids values) whose open command points to
; "$INSTDIR\block.exe" "%1". If deleting a ProgID fails (e.g. ACL denies
; deletion), advance the enumeration index so uninstall never spins and leave
; the deferred Active Setup PendingCleanup record to retry at user logon.
!macro UN_CLEAN_HKU_CLASSES_ROOT ROOT_SUBKEY LABEL_PREFIX
  StrCpy $6 0
  ${LABEL_PREFIX}_prog_loop:
    EnumRegKey $7 HKU "${ROOT_SUBKEY}" $6
    StrCmp $7 "" ${LABEL_PREFIX}_done
    StrCpy $8 $7 6
    StrCmp $8 "Block." 0 ${LABEL_PREFIX}_next_prog
    ReadRegStr $2 HKU "${ROOT_SUBKEY}\$7\shell\open\command" ""
    StrCmp $2 '"$INSTDIR\block.exe" "%1"' 0 ${LABEL_PREFIX}_next_prog
    ClearErrors
    DeleteRegKey HKU "${ROOT_SUBKEY}\$7"
    IfErrors ${LABEL_PREFIX}_delete_failed
    EnumRegKey $9 HKU "${ROOT_SUBKEY}" $6
    StrCmp $9 "$7" ${LABEL_PREFIX}_delete_failed ${LABEL_PREFIX}_clean_exts
  ${LABEL_PREFIX}_delete_failed:
    ClearErrors
    IntOp $6 $6 + 1
    Goto ${LABEL_PREFIX}_prog_loop
  ${LABEL_PREFIX}_clean_exts:
    StrCpy $3 0
    ${LABEL_PREFIX}_ext_loop:
      EnumRegKey $4 HKU "${ROOT_SUBKEY}" $3
      StrCmp $4 "" ${LABEL_PREFIX}_prog_loop
      IntOp $3 $3 + 1
      StrCpy $5 $4 1
      StrCmp $5 "." 0 ${LABEL_PREFIX}_ext_loop
      DeleteRegValue HKU "${ROOT_SUBKEY}\$4\OpenWithProgids" "$7"
      Goto ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_next_prog:
    IntOp $6 $6 + 1
    Goto ${LABEL_PREFIX}_prog_loop
  ${LABEL_PREFIX}_done:
    ClearErrors
!macroend

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; Block draws its own setup window (internal/installer): an interactive run
; unpacks block.exe to the plugins directory and starts it with --installer-ui.
; That window re-runs this executable with /S /D=<dir>, so the Install section
; below stays the single place that lays down files and registry keys. If the
; window cannot start (non-zero exit code), setup continues with the classic
; pages instead of leaving the user without a way to install.
Function .onInit
  IfSilent init_done
  InitPluginsDir
  ClearErrors
  File "/oname=$PLUGINSDIR\block-setup.exe" "${BINARY}"
  IfErrors init_done

  ; "/UPDATE" is passed by the in-app updater (internal/updater).
  StrCpy $R2 ""
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} "$R0" "/UPDATE" $R1
  IfErrors init_env
    StrCpy $R2 "1"
  init_env:
  ClearErrors

  ; Values travel through the environment: an install path ending in "\" or
  ; holding quotes cannot break the child's command line that way.
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_EXE", t "$EXEPATH")'
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_DIR", t "$INSTDIR")'
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_UPDATE", t "$R2")'

  StrCpy $R3 "1"
  ClearErrors
  ExecWait '"$PLUGINSDIR\block-setup.exe" --installer-ui' $R3
  IfErrors init_fallback
  StrCmp $R3 "0" 0 init_fallback
    ; Quit inside .onInit exits with 2 ("aborted by script") unless told otherwise.
    SetErrorLevel 0
    Quit
  init_fallback:
    ClearErrors
  init_done:
FunctionEnd

Section "Install"
  SetShellVarContext all
  !insertmacro BLOCK_COMPUTE_SCOPE_IDENTITY inst

  ; Cancel any deferred per-user cleanup scheduled by a prior uninstall of this
  ; exact installation directory before laying down files and registry keys
  ; (preserve CleanupRev so future uninstall cycles monotonically bump Active Setup Version).
  DeleteRegValue HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "ExecutablePath"
  DeleteRegKey HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID"
  Delete "$APPDATA\Block\cleanup-$BLOCK_SCOPE_ID.ps1"

  SetOutPath $INSTDIR
  File /oname=block.exe "${BINARY}"
  CreateShortcut "$BLOCK_SHORTCUT_PATH" "$INSTDIR\block.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Migrate legacy shared Uninstall\Block entry ONLY if it belonged to this exact $INSTDIR.
  ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
  StrCmp $0 "$INSTDIR" 0 inst_skip_legacy_uninst
    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block"
  inst_skip_legacy_uninst:

  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "DisplayName" "$BLOCK_DISPLAY_NAME"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "Publisher" "Block"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockScopeId" "$BLOCK_SCOPE_ID"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockMachineProgID" "$BLOCK_MACHINE_PROGID"
  WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath" "$BLOCK_SHORTCUT_PATH"
  WriteRegDWORD HKLM "$BLOCK_UNINSTALL_KEY" "NoModify" 1
  WriteRegDWORD HKLM "$BLOCK_UNINSTALL_KEY" "NoRepair" 1

  !include "${ASSOC_NSH}"
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  !insertmacro BLOCK_COMPUTE_SCOPE_IDENTITY un

  ; Resolve recorded shortcut and machine ProgID before removing uninstall metadata.
  ReadRegStr $R0 HKLM "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath"
  StrCmp $R0 "" 0 un_have_shortcut
    StrCpy $R0 "$BLOCK_SHORTCUT_PATH"
  un_have_shortcut:
  ReadRegStr $R1 HKLM "$BLOCK_UNINSTALL_KEY" "BlockMachineProgID"
  StrCmp $R1 "" 0 un_have_progid
    StrCpy $R1 "$BLOCK_MACHINE_PROGID"
  un_have_progid:

  Delete "$INSTDIR\block.exe"
  Delete "$INSTDIR\uninstall.exe"
  Delete "$R0"
  RMDir "$INSTDIR"

  ; Remove only this installation's uninstall registration (and legacy shared
  ; Uninstall\Block ONLY if its InstallLocation matches this exact $INSTDIR).
  ReadRegStr $0 HKLM "$BLOCK_UNINSTALL_KEY" "InstallLocation"
  StrCmp $0 "" un_del_scoped_uninst
  StrCmp $0 "$INSTDIR" un_del_scoped_uninst un_keep_scoped_uninst
  un_del_scoped_uninst:
    DeleteRegKey HKLM "$BLOCK_UNINSTALL_KEY"
  un_keep_scoped_uninst:

  ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
  StrCmp $0 "$INSTDIR" 0 un_keep_legacy_uninst
    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block"
  un_keep_legacy_uninst:

  ; Clean this installation's machine-wide ProgID in HKLM (plus legacy shared
  ; Block.Machine.Document / Block.Document only if they point to this $INSTDIR).
  DeleteRegKey HKLM "Software\Block\MachineAssoc\$BLOCK_SCOPE_ID"
  !insertmacro UN_CLEAN_HKLM_PROGID "$R1" un_hklm_scoped
  !insertmacro UN_CLEAN_HKLM_PROGID "Block.Machine.Document" un_hklm_legacy_machine
  !insertmacro UN_CLEAN_HKLM_PROGID "Block.Document" un_hklm_legacy_doc

  ; Immediately clean every Block.* per-user ProgID across all currently loaded
  ; user hives (HKU\<SID>_Classes and HKU\<SID>\Software\Classes) whose open
  ; command points to this uninstalled "$INSTDIR\block.exe" "%1".
  StrCpy $0 0
  un_user_loop:
    EnumRegKey $1 HKU "" $0
    StrCmp $1 "" un_user_done
    IntOp $0 $0 + 1
    !insertmacro UN_CLEAN_HKU_CLASSES_ROOT "$1" un_hku_direct
    !insertmacro UN_CLEAN_HKU_CLASSES_ROOT "$1\Software\Classes" un_hku_sw
    Goto un_user_loop
  un_user_done:

  ; Schedule deferred per-user cleanup via Windows Active Setup so users who
  ; were logged out during uninstall (whose HKCU hives were not loaded in HKU)
  ; automatically purge stale Block.* ProgIDs pointing to "$INSTDIR\block.exe"
  ; on their next logon. The script verifies the uninstalled binary is still
  ; absent so a subsequent reinstall to the same directory is never affected.
  ReadRegDWORD $R9 HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "CleanupRev"
  IntOp $R9 $R9 + 1
  WriteRegStr HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "ExecutablePath" "$INSTDIR\block.exe"
  WriteRegStr HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "ScopeId" "$BLOCK_SCOPE_ID"
  WriteRegDWORD HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "CleanupRev" $R9

  CreateDirectory "$APPDATA\Block"
  FileOpen $9 "$APPDATA\Block\cleanup-$BLOCK_SCOPE_ID.ps1" w
  StrCmp $9 "" un_skip_deferred
  FileWrite $9 '$$ErrorActionPreference = "SilentlyContinue"$\r$\n'
  FileWrite $9 '$$meta = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey("Software\Block\PendingCleanup\$BLOCK_SCOPE_ID", $$false)$\r$\n'
  FileWrite $9 'if ($$null -eq $$meta) { exit 0 }$\r$\n'
  FileWrite $9 '$$targetExe = [string]$$meta.GetValue("ExecutablePath", "")$\r$\n'
  FileWrite $9 '$$meta.Close()$\r$\n'
  FileWrite $9 'if ([string]::IsNullOrWhiteSpace($$targetExe)) { exit 0 }$\r$\n'
  FileWrite $9 'if (Test-Path -LiteralPath $$targetExe) { exit 0 }$\r$\n'
  FileWrite $9 '$$q = [char]34$\r$\n'
  FileWrite $9 '$$expectedCmd = ($$q + $$targetExe + $$q + " " + $$q + "%1" + $$q).ToLowerInvariant()$\r$\n'
  FileWrite $9 '$$hkcu = [Microsoft.Win32.Registry]::CurrentUser$\r$\n'
  FileWrite $9 '$$cls = $$hkcu.OpenSubKey("Software\Classes", $$true)$\r$\n'
  FileWrite $9 'if ($$null -eq $$cls) { exit 0 }$\r$\n'
  FileWrite $9 '$$removed = New-Object System.Collections.Generic.List[string]$\r$\n'
  FileWrite $9 'foreach ($$name in $$cls.GetSubKeyNames()) {$\r$\n'
  FileWrite $9 '  if (-not $$name.StartsWith("Block.", [System.StringComparison]::OrdinalIgnoreCase)) { continue }$\r$\n'
  FileWrite $9 '  $$ck = $$cls.OpenSubKey($$name + "\shell\open\command", $$false)$\r$\n'
  FileWrite $9 '  if ($$null -eq $$ck) { continue }$\r$\n'
  FileWrite $9 '  $$cmd = [string]$$ck.GetValue("", "")$\r$\n'
  FileWrite $9 '  $$ck.Close()$\r$\n'
  FileWrite $9 '  if ($$cmd.Trim().ToLowerInvariant() -eq $$expectedCmd) {$\r$\n'
  FileWrite $9 '    try { $$cls.DeleteSubKeyTree($$name, $$false) } catch {}$\r$\n'
  FileWrite $9 '    [void]$$removed.Add($$name)$\r$\n'
  FileWrite $9 '  }$\r$\n'
  FileWrite $9 '}$\r$\n'
  FileWrite $9 'if ($$removed.Count -gt 0) {$\r$\n'
  FileWrite $9 '  foreach ($$ext in $$cls.GetSubKeyNames()) {$\r$\n'
  FileWrite $9 '    if (-not $$ext.StartsWith(".")) { continue }$\r$\n'
  FileWrite $9 '    $$ow = $$cls.OpenSubKey($$ext + "\OpenWithProgids", $$true)$\r$\n'
  FileWrite $9 '    if ($$null -eq $$ow) { continue }$\r$\n'
  FileWrite $9 '    foreach ($$pid in $$removed) { try { $$ow.DeleteValue($$pid, $$false) } catch {} }$\r$\n'
  FileWrite $9 '    $$ow.Close()$\r$\n'
  FileWrite $9 '  }$\r$\n'
  FileWrite $9 '  try {$\r$\n'
  FileWrite $9 '    $$sig = "[System.Runtime.InteropServices.DllImport(" + $$q + "shell32.dll" + $$q + ")] public static extern void SHChangeNotify(int wEventId, uint uFlags, System.IntPtr dwItem1, System.IntPtr dwItem2);"$\r$\n'
  FileWrite $9 '    Add-Type -Namespace BlockClean -Name Shell32 -MemberDefinition $$sig -ErrorAction SilentlyContinue$\r$\n'
  FileWrite $9 '    [BlockClean.Shell32]::SHChangeNotify(0x08000000, 0, [System.IntPtr]::Zero, [System.IntPtr]::Zero)$\r$\n'
  FileWrite $9 '  } catch {}$\r$\n'
  FileWrite $9 '}$\r$\n'
  FileWrite $9 '$$cls.Close()$\r$\n'
  FileClose $9

  WriteRegStr HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID" "" "Block File Association Cleanup"
  WriteRegDWORD HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID" "IsInstalled" 1
  WriteRegStr HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID" "Version" "$R9,0"
  WriteRegStr HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID" "StubPath" 'powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File "$APPDATA\Block\cleanup-$BLOCK_SCOPE_ID.ps1"'
  un_skip_deferred:

  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd
