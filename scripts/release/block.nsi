Unicode true
!include "MUI2.nsh"
!include "FileFunc.nsh"
!insertmacro un.GetParameters
!insertmacro un.GetOptions

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
; Leave InstallDir unset so $INSTDIR starts empty unless /D= was explicitly
; passed on the command line (NSIS strips /D= from $CMDLINE before .onInit).
RequestExecutionLevel user

Var BLOCK_NORM_DIR
Var BLOCK_SCOPE_ID
Var BLOCK_INSTALL_SCOPE
Var BLOCK_MACHINE_PROGID
Var BLOCK_USER_PROGID
Var BLOCK_UNINSTALL_KEY
Var BLOCK_SHORTCUT_PATH
Var BLOCK_DISPLAY_NAME
Var BLOCK_IS_UPDATE
Var BLOCK_FORCE_ALLUSERS
Var BLOCK_IS_ADMIN
Var BLOCK_FAIL_REASON

!macro BLOCK_CHECK_ADMIN PREFIX
  Push $0
  System::Call 'shell32::IsUserAnAdmin() i .r0'
  IntCmp $0 0 ${PREFIX}_not_admin
    StrCpy $BLOCK_IS_ADMIN "1"
    Goto ${PREFIX}_done
  ${PREFIX}_not_admin:
    StrCpy $BLOCK_IS_ADMIN "0"
  ${PREFIX}_done:
  Pop $0
!macroend

; Compute deterministic 16-hex-char scope ID from lowercased $INSTDIR (with
; trailing backslashes stripped) using Windows shlwapi::HashData so coexisting
; installs (e.g. C:\Block-A and D:\Block-B) always receive unique ProgIDs,
; uninstall entries, shortcuts, and deferred per-user cleanup tasks.
!macro BLOCK_COMPUTE_SCOPE_HASH PREFIX
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

  StrCpy $BLOCK_NORM_DIR "$0"
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

  Pop $5
  Pop $4
  Pop $3
  Pop $2
  Pop $1
  Pop $0
!macroend

; Check whether $BLOCK_NORM_DIR equals or is nested inside BASE_DIR.
; Sets $R8 to "1" on match, leaves $R8 unchanged otherwise.
!macro BLOCK_CHECK_UNDER_DIR BASE_DIR PREFIX
  Push $0
  Push $1
  Push $2
  Push $3

  StrCpy $0 "${BASE_DIR}"
  ${PREFIX}_strip:
    StrLen $1 $0
    IntCmp $1 3 ${PREFIX}_stripped ${PREFIX}_stripped 0
    StrCpy $2 $0 1 -1
    StrCmp $2 "\" 0 ${PREFIX}_stripped
    IntOp $1 $1 - 1
    StrCpy $0 $0 $1
    Goto ${PREFIX}_strip
  ${PREFIX}_stripped:
  StrCmp $0 "" ${PREFIX}_done

  StrLen $1 $0
  StrCpy $2 "$BLOCK_NORM_DIR" $1
  StrCmp $2 "$0" 0 ${PREFIX}_done
  StrLen $3 "$BLOCK_NORM_DIR"
  IntCmp $3 $1 ${PREFIX}_match ${PREFIX}_done 0
  StrCpy $2 "$BLOCK_NORM_DIR" 1 $1
  StrCmp $2 "\" ${PREFIX}_match ${PREFIX}_done
  ${PREFIX}_match:
    StrCpy $R8 "1"
  ${PREFIX}_done:

  Pop $3
  Pop $2
  Pop $1
  Pop $0
!macroend

; Decide whether $INSTDIR targets a machine-wide (HKLM) or per-user (HKCU)
; installation. Requires BLOCK_COMPUTE_SCOPE_HASH to have run first.
!macro BLOCK_DETECT_INSTALL_SCOPE PREFIX
  Push $0
  Push $R8

  StrCpy $BLOCK_INSTALL_SCOPE "user"
  StrCmp $BLOCK_FORCE_ALLUSERS "1" ${PREFIX}_machine 0

  ClearErrors
  ReadRegStr $0 HKLM "$BLOCK_UNINSTALL_KEY" "InstallLocation"
  IfErrors ${PREFIX}_check_legacy_hklm
  StrCmp $0 "$INSTDIR" ${PREFIX}_machine 0
  StrCmp $0 "$BLOCK_NORM_DIR" ${PREFIX}_machine 0

  ${PREFIX}_check_legacy_hklm:
  ClearErrors
  ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
  IfErrors ${PREFIX}_check_progfiles
  StrCmp $0 "$INSTDIR" ${PREFIX}_machine 0
  StrCmp $0 "$BLOCK_NORM_DIR" ${PREFIX}_machine 0

  ${PREFIX}_check_progfiles:
  StrCpy $R8 "0"
  !insertmacro BLOCK_CHECK_UNDER_DIR "$PROGRAMFILES64" ${PREFIX}_pf64
  !insertmacro BLOCK_CHECK_UNDER_DIR "$PROGRAMFILES" ${PREFIX}_pf32
  StrCmp $R8 "1" ${PREFIX}_machine ${PREFIX}_done

  ${PREFIX}_machine:
    StrCpy $BLOCK_INSTALL_SCOPE "machine"
  ${PREFIX}_done:
  ClearErrors

  Pop $R8
  Pop $0
!macroend

; Compute shortcut path, display name, and per-user ProgID after
; SetShellVarContext matches $BLOCK_INSTALL_SCOPE.
!macro BLOCK_COMPUTE_SCOPE_IDENTITY PREFIX
  StrCmp $BLOCK_INSTALL_SCOPE "machine" ${PREFIX}_machine ${PREFIX}_user
  ${PREFIX}_machine:
    StrCpy $BLOCK_USER_PROGID ""
    StrCmp $BLOCK_NORM_DIR "$PROGRAMFILES64\Block" 0 ${PREFIX}_machine_custom
      StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block.lnk"
      StrCpy $BLOCK_DISPLAY_NAME "Block"
      Goto ${PREFIX}_done
    ${PREFIX}_machine_custom:
      StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block-$BLOCK_SCOPE_ID.lnk"
      StrCpy $BLOCK_DISPLAY_NAME "Block ($INSTDIR)"
      Goto ${PREFIX}_done
  ${PREFIX}_user:
    StrCmp $BLOCK_NORM_DIR "$LOCALAPPDATA\Programs\Block" 0 ${PREFIX}_user_custom
      StrCpy $BLOCK_USER_PROGID "Block.User.Document"
      StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block.lnk"
      StrCpy $BLOCK_DISPLAY_NAME "Block"
      Goto ${PREFIX}_done
    ${PREFIX}_user_custom:
      StrCpy $BLOCK_USER_PROGID "Block.User.$BLOCK_SCOPE_ID"
      StrCpy $BLOCK_SHORTCUT_PATH "$SMPROGRAMS\Block-$BLOCK_SCOPE_ID.lnk"
      StrCpy $BLOCK_DISPLAY_NAME "Block ($INSTDIR)"
  ${PREFIX}_done:
!macroend

; Wait up to 20 seconds for an existing $INSTDIR\block.exe to release its file
; lock (e.g. while the running app exits during a seamless in-app update).
!macro BLOCK_WAIT_EXE_UNLOCKED PREFIX
  Push $0
  Push $1

  StrCpy $0 100
  ${PREFIX}_loop:
    IfFileExists "$INSTDIR\block.exe" 0 ${PREFIX}_done
    System::Call 'kernel32::CreateFileW(t "$INSTDIR\block.exe", i 0x40000000, i 0, p 0, i 3, i 0, p 0) p .r1'
    IntCmp $1 -1 ${PREFIX}_locked ${PREFIX}_unlocked ${PREFIX}_unlocked
  ${PREFIX}_unlocked:
    System::Call 'kernel32::CloseHandle(p r1)'
    Goto ${PREFIX}_done
  ${PREFIX}_locked:
    IntOp $0 $0 - 1
    IntCmp $0 0 ${PREFIX}_done ${PREFIX}_done 0
    Sleep 200
    Goto ${PREFIX}_loop
  ${PREFIX}_done:

  Pop $1
  Pop $0
!macroend

; Wait up to 2 seconds for an existing $INSTDIR\block.exe to release its file
; lock during uninstall (e.g. if the user closed Block right before uninstalling).
!macro BLOCK_WAIT_EXE_UNLOCKED_SHORT PREFIX
  Push $0
  Push $1

  StrCpy $0 10
  ${PREFIX}_loop:
    IfFileExists "$INSTDIR\block.exe" 0 ${PREFIX}_done
    System::Call 'kernel32::CreateFileW(t "$INSTDIR\block.exe", i 0x40000000, i 0, p 0, i 3, i 0, p 0) p .r1'
    IntCmp $1 -1 ${PREFIX}_locked ${PREFIX}_unlocked ${PREFIX}_unlocked
  ${PREFIX}_unlocked:
    System::Call 'kernel32::CloseHandle(p r1)'
    Goto ${PREFIX}_done
  ${PREFIX}_locked:
    IntOp $0 $0 - 1
    IntCmp $0 0 ${PREFIX}_done ${PREFIX}_done 0
    Sleep 200
    Goto ${PREFIX}_loop
  ${PREFIX}_done:

  Pop $1
  Pop $0
!macroend

; Reconcile stale HKLM OpenWithProgids values for PROGID during an in-place
; machine-wide upgrade while preserving extensions selected as a default handler.
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

; Reconcile stale HKCU OpenWithProgids values for PROGID during an in-place
; per-user upgrade while preserving extensions selected as a default handler.
!macro INST_RECONCILE_HKCU_PROGID PROGID NEW_EXTS_KEY LABEL_PREFIX
  Push $3
  Push $4
  Push $5
  Push $8

  StrCpy $3 0
  ${LABEL_PREFIX}_ext_loop:
    EnumRegKey $4 HKCU "Software\Classes" $3
    StrCmp $4 "" ${LABEL_PREFIX}_done
    IntOp $3 $3 + 1
    StrCpy $5 $4 1
    StrCmp $5 "." 0 ${LABEL_PREFIX}_ext_loop
    ClearErrors
    ReadRegStr $5 HKCU "Software\Classes\$4\OpenWithProgids" "${PROGID}"
    IfErrors ${LABEL_PREFIX}_ext_loop
    ClearErrors
    ReadRegStr $5 HKCU "${NEW_EXTS_KEY}" "$4"
    IfErrors ${LABEL_PREFIX}_check_default ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_check_default:
    ClearErrors
    ReadRegStr $8 HKCU "Software\Classes\$4" ""
    StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
    ReadRegStr $8 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\$4\UserChoice" "ProgId"
    StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
    ReadRegStr $8 HKCU "Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\$4\UserChoiceLatest\ProgId" "ProgId"
    StrCmp $8 "${PROGID}" ${LABEL_PREFIX}_ext_loop
    DeleteRegValue HKCU "Software\Classes\$4\OpenWithProgids" "${PROGID}"
    Goto ${LABEL_PREFIX}_ext_loop
  ${LABEL_PREFIX}_done:
    ClearErrors

  Pop $8
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
; "$INSTDIR\block.exe" "%1".
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

; Enumerate every Block.* ProgID under HKCU\Software\Classes and remove any
; ProgID (plus its .ext\OpenWithProgids values) pointing to "$INSTDIR\block.exe" "%1".
!macro UN_CLEAN_HKCU_CLASSES_ROOT LABEL_PREFIX
  StrCpy $6 0
  ${LABEL_PREFIX}_prog_loop:
    EnumRegKey $7 HKCU "Software\Classes" $6
    StrCmp $7 "" ${LABEL_PREFIX}_done
    StrCpy $8 $7 6
    StrCmp $8 "Block." 0 ${LABEL_PREFIX}_next_prog
    ReadRegStr $2 HKCU "Software\Classes\$7\shell\open\command" ""
    StrCmp $2 '"$INSTDIR\block.exe" "%1"' 0 ${LABEL_PREFIX}_next_prog
    ClearErrors
    DeleteRegKey HKCU "Software\Classes\$7"
    IfErrors ${LABEL_PREFIX}_delete_failed
    EnumRegKey $9 HKCU "Software\Classes" $6
    StrCmp $9 "$7" ${LABEL_PREFIX}_delete_failed ${LABEL_PREFIX}_clean_exts
  ${LABEL_PREFIX}_delete_failed:
    ClearErrors
    IntOp $6 $6 + 1
    Goto ${LABEL_PREFIX}_prog_loop
  ${LABEL_PREFIX}_clean_exts:
    StrCpy $3 0
    ${LABEL_PREFIX}_ext_loop:
      EnumRegKey $4 HKCU "Software\Classes" $3
      StrCmp $4 "" ${LABEL_PREFIX}_prog_loop
      IntOp $3 $3 + 1
      StrCpy $5 $4 1
      StrCmp $5 "." 0 ${LABEL_PREFIX}_ext_loop
      DeleteRegValue HKCU "Software\Classes\$4\OpenWithProgids" "$7"
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
; English stays first as the fallback; NSIS automatically selects the table
; matching the user's Windows display language for both setup and uninstaller.
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "TradChinese"
!insertmacro MUI_LANGUAGE "Japanese"
!insertmacro MUI_LANGUAGE "Korean"
!insertmacro MUI_LANGUAGE "French"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "Spanish"
!insertmacro MUI_LANGUAGE "SpanishInternational"
!insertmacro MUI_LANGUAGE "Portuguese"
!insertmacro MUI_LANGUAGE "PortugueseBR"
!insertmacro MUI_LANGUAGE "Russian"
!insertmacro MUI_LANGUAGE "Italian"

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=${LANG_ENGLISH} "ProductName" "Block"
VIAddVersionKey /LANG=${LANG_ENGLISH} "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_ENGLISH} "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_ENGLISH} "FileDescription" "Block installer"

LangString UN_EXE_IN_USE ${LANG_ENGLISH} "Block is still running. Please close Block and try uninstalling again."
LangString UN_EXE_IN_USE ${LANG_SIMPCHINESE} "Block 正在运行，请先关闭 Block 后重试卸载。"
LangString UN_EXE_IN_USE ${LANG_TRADCHINESE} "Block 正在執行，請先關閉 Block 後再重試解除安裝。"
LangString UN_EXE_IN_USE ${LANG_JAPANESE} "Block が実行中です。Block を終了してからアンインストールを再試行してください。"
LangString UN_EXE_IN_USE ${LANG_KOREAN} "Block이 실행 중입니다. Block을 종료한 후 다시 제거해 주세요."
LangString UN_EXE_IN_USE ${LANG_FRENCH} "Block est encore en cours d'exécution. Fermez Block puis relancez la désinstallation."
LangString UN_EXE_IN_USE ${LANG_GERMAN} "Block wird noch ausgeführt. Schließen Sie Block und versuchen Sie die Deinstallation erneut."
LangString UN_EXE_IN_USE ${LANG_SPANISH} "Block sigue ejecutándose. Cierra Block e intenta desinstalarlo de nuevo."
LangString UN_EXE_IN_USE ${LANG_SPANISHINTERNATIONAL} "Block sigue ejecutándose. Cierra Block e intenta desinstalarlo de nuevo."
LangString UN_EXE_IN_USE ${LANG_PORTUGUESE} "O Block ainda está em execução. Feche o Block e tente desinstalar novamente."
LangString UN_EXE_IN_USE ${LANG_PORTUGUESEBR} "O Block ainda está em execução. Feche o Block e tente desinstalar novamente."
LangString UN_EXE_IN_USE ${LANG_RUSSIAN} "Block всё ещё запущен. Закройте Block и повторите удаление."
LangString UN_EXE_IN_USE ${LANG_ITALIAN} "Block è ancora in esecuzione. Chiudi Block e riprova a disinstallarlo."

LangString INST_FAIL_GENERIC ${LANG_ENGLISH} "Failed to install Block."
LangString INST_FAIL_GENERIC ${LANG_SIMPCHINESE} "安装 Block 失败。"
LangString INST_FAIL_GENERIC ${LANG_TRADCHINESE} "安裝 Block 失敗。"
LangString INST_FAIL_GENERIC ${LANG_JAPANESE} "Block のインストールに失敗しました。"
LangString INST_FAIL_GENERIC ${LANG_KOREAN} "Block 설치에 실패했습니다."
LangString INST_FAIL_GENERIC ${LANG_FRENCH} "Échec de l'installation de Block."
LangString INST_FAIL_GENERIC ${LANG_GERMAN} "Block konnte nicht installiert werden."
LangString INST_FAIL_GENERIC ${LANG_SPANISH} "No se pudo instalar Block."
LangString INST_FAIL_GENERIC ${LANG_SPANISHINTERNATIONAL} "No se pudo instalar Block."
LangString INST_FAIL_GENERIC ${LANG_PORTUGUESE} "Falha ao instalar o Block."
LangString INST_FAIL_GENERIC ${LANG_PORTUGUESEBR} "Falha ao instalar o Block."
LangString INST_FAIL_GENERIC ${LANG_RUSSIAN} "Не удалось установить Block."
LangString INST_FAIL_GENERIC ${LANG_ITALIAN} "Installazione di Block non riuscita."

LangString INST_FAIL_ADMIN ${LANG_ENGLISH} "Administrator privileges are required to update Block in $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_SIMPCHINESE} "更新 $INSTDIR 中的 Block 需要管理员权限。"
LangString INST_FAIL_ADMIN ${LANG_TRADCHINESE} "更新 $INSTDIR 中的 Block 需要系統管理員權限。"
LangString INST_FAIL_ADMIN ${LANG_JAPANESE} "$INSTDIR の Block を更新するには管理者権限が必要です。"
LangString INST_FAIL_ADMIN ${LANG_KOREAN} "$INSTDIR의 Block을 업데이트하려면 관리자 권한이 필요합니다."
LangString INST_FAIL_ADMIN ${LANG_FRENCH} "Des droits d'administrateur sont requis pour mettre à jour Block dans $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_GERMAN} "Für das Aktualisieren von Block in $INSTDIR sind Administratorrechte erforderlich."
LangString INST_FAIL_ADMIN ${LANG_SPANISH} "Se requieren privilegios de administrador para actualizar Block en $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_SPANISHINTERNATIONAL} "Se requieren privilegios de administrador para actualizar Block en $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_PORTUGUESE} "São necessários privilégios de administrador para atualizar o Block em $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_PORTUGUESEBR} "São necessários privilégios de administrador para atualizar o Block em $INSTDIR."
LangString INST_FAIL_ADMIN ${LANG_RUSSIAN} "Для обновления Block в $INSTDIR требуются права администратора."
LangString INST_FAIL_ADMIN ${LANG_ITALIAN} "Sono necessari i privilegi di amministratore per aggiornare Block in $INSTDIR."

LangString INST_FAIL_UNSAFE_DIR ${LANG_ENGLISH} "System-wide installation requires an administrator-protected directory."
LangString INST_FAIL_UNSAFE_DIR ${LANG_SIMPCHINESE} "全局安装必须选择受管理员保护的目录。"
LangString INST_FAIL_UNSAFE_DIR ${LANG_TRADCHINESE} "全域安裝必須選擇受系統管理員保護的目錄。"
LangString INST_FAIL_UNSAFE_DIR ${LANG_JAPANESE} "全ユーザー向けインストールには管理者保護されたフォルダーが必要です。"
LangString INST_FAIL_UNSAFE_DIR ${LANG_KOREAN} "시스템 전체 설치에는 관리자 보호 폴더가 필요합니다."
LangString INST_FAIL_UNSAFE_DIR ${LANG_FRENCH} "L'installation globale nécessite un dossier protégé par administrateur."
LangString INST_FAIL_UNSAFE_DIR ${LANG_GERMAN} "Für eine systemweite Installation ist ein administratorgeschützter Ordner erforderlich."
LangString INST_FAIL_UNSAFE_DIR ${LANG_SPANISH} "La instalación para todo el sistema requiere una carpeta protegida por administrador."
LangString INST_FAIL_UNSAFE_DIR ${LANG_SPANISHINTERNATIONAL} "La instalación para todo el sistema requiere una carpeta protegida por administrador."
LangString INST_FAIL_UNSAFE_DIR ${LANG_PORTUGUESE} "A instalação para todo o sistema requer uma pasta protegida por administrador."
LangString INST_FAIL_UNSAFE_DIR ${LANG_PORTUGUESEBR} "A instalação para todo o sistema requer uma pasta protegida por administrador."
LangString INST_FAIL_UNSAFE_DIR ${LANG_RUSSIAN} "Для установки для всех пользователей требуется защищённая администратором папка."
LangString INST_FAIL_UNSAFE_DIR ${LANG_ITALIAN} "L'installazione di sistema richiede una cartella protetta da amministratore."

LangString INST_FAIL_RELAUNCH ${LANG_ENGLISH} "Updated Block failed to launch; restored previous version."
LangString INST_FAIL_RELAUNCH ${LANG_SIMPCHINESE} "更新后的 Block 启动失败，已恢复到上一版本。"
LangString INST_FAIL_RELAUNCH ${LANG_TRADCHINESE} "更新後的 Block 啟動失敗，已還原至上一版本。"
LangString INST_FAIL_RELAUNCH ${LANG_JAPANESE} "更新後の Block の起動に失敗したため、以前のバージョンを復元しました。"
LangString INST_FAIL_RELAUNCH ${LANG_KOREAN} "업데이트된 Block을 실행하지 못해 이전 버전으로 복원했습니다."
LangString INST_FAIL_RELAUNCH ${LANG_FRENCH} "Le lancement de la nouvelle version de Block a échoué ; la version précédente a été restaurée."
LangString INST_FAIL_RELAUNCH ${LANG_GERMAN} "Die aktualisierte Version von Block konnte nicht gestartet werden; vorherige Version wiederhergestellt."
LangString INST_FAIL_RELAUNCH ${LANG_SPANISH} "No se pudo iniciar la nueva versión de Block; se restauró la versión anterior."
LangString INST_FAIL_RELAUNCH ${LANG_SPANISHINTERNATIONAL} "No se pudo iniciar la nueva versión de Block; se restauró la versión anterior."
LangString INST_FAIL_RELAUNCH ${LANG_PORTUGUESE} "Falha ao iniciar o Block atualizado; a versão anterior foi restaurada."
LangString INST_FAIL_RELAUNCH ${LANG_PORTUGUESEBR} "Falha ao iniciar o Block atualizado; a versão anterior foi restaurada."
LangString INST_FAIL_RELAUNCH ${LANG_RUSSIAN} "Не удалось запустить обновлённый Block; восстановлена предыдущая версия."
LangString INST_FAIL_RELAUNCH ${LANG_ITALIAN} "Impossibile avviare la nuova versione di Block; versione precedente ripristinata."

; Match $LANGUAGE against the full Windows preferred UI language list
; (GetUserPreferredUILanguages with MUI_LANGUAGE_NAME) so both fallback setup
; pages and the uninstaller follow the exact same preference order as Block's
; custom setup window (e.g. ca-ES -> es-ES resolves to Spanish instead of
; falling straight to English).
!macro BLOCK_SELECT_PREFERRED_LANGUAGE PREFIX
  Push $0
  Push $1
  Push $2
  Push $3
  Push $4
  Push $5
  Push $6
  Push $7
  Push $8
  Push $9

  StrCpy $0 0
  StrCpy $1 0
  System::Call 'kernel32::GetUserPreferredUILanguages(i 8, *i r0 .r0, p 0, *i r1 .r1) i .r2'
  IntCmp $2 0 ${PREFIX}_done
  IntCmp $1 1 ${PREFIX}_done ${PREFIX}_done 0

  IntOp $3 $1 + 2
  IntOp $3 $3 * 2
  System::Alloc $3
  Pop $4
  IntCmp $4 0 ${PREFIX}_done

  System::Call 'kernel32::GetUserPreferredUILanguages(i 8, *i r0 .r0, p r4, *i r1 .r1) i .r2'
  IntCmp $2 0 ${PREFIX}_free

  StrCpy $5 $4
  ${PREFIX}_next_tag:
    System::Call 'kernel32::lstrlenW(p r5) i .r7'
    IntCmp $7 0 ${PREFIX}_fallback_en
    System::Call 'kernel32::lstrcpynW(t .r6, p r5, i 128)'
    IntOp $7 $7 + 1
    IntOp $7 $7 * 2
    IntOp $5 $5 + $7

    StrCpy $8 "$6-" 3
    StrCmp $8 "en-" 0 +3
      StrCpy $LANGUAGE ${LANG_ENGLISH}
      Goto ${PREFIX}_free
    StrCmp $8 "ja-" 0 +3
      StrCpy $LANGUAGE ${LANG_JAPANESE}
      Goto ${PREFIX}_free
    StrCmp $8 "ko-" 0 +3
      StrCpy $LANGUAGE ${LANG_KOREAN}
      Goto ${PREFIX}_free
    StrCmp $8 "fr-" 0 +3
      StrCpy $LANGUAGE ${LANG_FRENCH}
      Goto ${PREFIX}_free
    StrCmp $8 "de-" 0 +3
      StrCpy $LANGUAGE ${LANG_GERMAN}
      Goto ${PREFIX}_free
    StrCmp $8 "ru-" 0 +3
      StrCpy $LANGUAGE ${LANG_RUSSIAN}
      Goto ${PREFIX}_free
    StrCmp $8 "it-" 0 +3
      StrCpy $LANGUAGE ${LANG_ITALIAN}
      Goto ${PREFIX}_free
    StrCmp $8 "es-" ${PREFIX}_match_es 0
    StrCmp $8 "pt-" ${PREFIX}_match_pt 0
    StrCmp $8 "zh-" ${PREFIX}_match_zh ${PREFIX}_next_tag

  ${PREFIX}_match_es:
    StrCmp $6 "es" ${PREFIX}_es_spain 0
    StrCmp $6 "es-ES" ${PREFIX}_es_spain 0
    StrCpy $LANGUAGE ${LANG_SPANISHINTERNATIONAL}
    Goto ${PREFIX}_free
  ${PREFIX}_es_spain:
    StrCpy $LANGUAGE ${LANG_SPANISH}
    Goto ${PREFIX}_free

  ${PREFIX}_match_pt:
    StrCmp $6 "pt-BR" 0 ${PREFIX}_pt_std
    StrCpy $LANGUAGE ${LANG_PORTUGUESEBR}
    Goto ${PREFIX}_free
  ${PREFIX}_pt_std:
    StrCpy $LANGUAGE ${LANG_PORTUGUESE}
    Goto ${PREFIX}_free

  ; Inspect zh-* subtags with explicit script (Hans/Hant) taking priority over region.
  ${PREFIX}_match_zh:
    StrCpy $9 "0" ; 1 if TradChinese region (TW/HK/MO) seen
    StrCpy $2 "$6" "" 3 ; remainder after "zh-"
  ${PREFIX}_zh_loop:
    StrCmp $2 "" ${PREFIX}_zh_finish
    StrCpy $3 0
    StrLen $0 $2
  ${PREFIX}_zh_scan:
    IntCmp $3 $0 ${PREFIX}_zh_got_tok ${PREFIX}_zh_got_tok 0
    StrCpy $8 $2 1 $3
    StrCmp $8 "-" ${PREFIX}_zh_got_tok 0
    StrCmp $8 "_" ${PREFIX}_zh_got_tok 0
    IntOp $3 $3 + 1
    Goto ${PREFIX}_zh_scan
  ${PREFIX}_zh_got_tok:
    StrCpy $8 $2 $3
    IntOp $3 $3 + 1
    StrCpy $2 $2 "" $3
    StrCmp $8 "Hans" ${PREFIX}_zh_simp 0
    StrCmp $8 "Hant" ${PREFIX}_zh_trad 0
    StrCmp $8 "TW" ${PREFIX}_zh_mark_trad 0
    StrCmp $8 "HK" ${PREFIX}_zh_mark_trad 0
    StrCmp $8 "MO" ${PREFIX}_zh_mark_trad ${PREFIX}_zh_loop
  ${PREFIX}_zh_mark_trad:
    StrCpy $9 "1"
    Goto ${PREFIX}_zh_loop
  ${PREFIX}_zh_finish:
    StrCmp $9 "1" ${PREFIX}_zh_trad ${PREFIX}_zh_simp
  ${PREFIX}_zh_trad:
    StrCpy $LANGUAGE ${LANG_TRADCHINESE}
    Goto ${PREFIX}_free
  ${PREFIX}_zh_simp:
    StrCpy $LANGUAGE ${LANG_SIMPCHINESE}
    Goto ${PREFIX}_free

  ${PREFIX}_fallback_en:
    StrCpy $LANGUAGE ${LANG_ENGLISH}

  ${PREFIX}_free:
    System::Free $4
  ${PREFIX}_done:
  Pop $9
  Pop $8
  Pop $7
  Pop $6
  Pop $5
  Pop $4
  Pop $3
  Pop $2
  Pop $1
  Pop $0
!macroend

; Block defaults to a per-user install in %LOCALAPPDATA%\Programs\Block so
; in-app updates run seamlessly with no installer window and no UAC prompt.
; Machine-wide installs (e.g. C:\Program Files\Block or /ALLUSERS) remain fully
; supported: interactive installs elevate on demand, and /UPDATE runs completely
; silently before relaunching Block.
Function .onInit
  !insertmacro BLOCK_SELECT_PREFERRED_LANGUAGE init_lang
  StrCpy $BLOCK_IS_UPDATE ""
  StrCpy $BLOCK_FORCE_ALLUSERS ""

  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} "$R0" "/UPDATE" $R1
  IfErrors init_check_allusers
    StrCpy $BLOCK_IS_UPDATE "1"
  init_check_allusers:
  ClearErrors
  ${GetOptions} "$R0" "/ALLUSERS" $R1
  IfErrors init_check_existing_dir
    StrCpy $BLOCK_FORCE_ALLUSERS "1"
  init_check_existing_dir:
  ClearErrors

  ; InstallDir is intentionally unset, so a non-empty $INSTDIR here means /D=
  ; was explicitly passed on the command line and must never be overridden.
  StrCmp $INSTDIR "" 0 init_dir_ready
  StrCmp $BLOCK_FORCE_ALLUSERS "1" init_default_machine 0
  IfFileExists "$LOCALAPPDATA\Programs\Block\block.exe" init_default_user 0
  IfFileExists "$PROGRAMFILES64\Block\block.exe" 0 init_check_legacy_hklm
    StrCpy $INSTDIR "$PROGRAMFILES64\Block"
    Goto init_dir_ready
  init_check_legacy_hklm:
  ReadRegStr $R4 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
  StrCmp $R4 "" init_check_scoped_hklm 0
  IfFileExists "$R4\block.exe" 0 init_check_scoped_hklm
    StrCpy $INSTDIR "$R4"
    Goto init_dir_ready
  init_check_scoped_hklm:
  StrCpy $R5 0
  init_hklm_loop:
    EnumRegKey $R6 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall" $R5
    StrCmp $R6 "" init_default_user
    IntOp $R5 $R5 + 1
    StrCpy $R7 $R6 6
    StrCmp $R7 "Block-" 0 init_hklm_loop
    ReadRegStr $R4 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\$R6" "InstallLocation"
    StrCmp $R4 "" init_hklm_loop 0
    IfFileExists "$R4\block.exe" 0 init_hklm_loop
      StrCpy $INSTDIR "$R4"
      Goto init_dir_ready
  init_default_user:
    StrCpy $INSTDIR "$LOCALAPPDATA\Programs\Block"
    Goto init_dir_ready
  init_default_machine:
    StrCpy $INSTDIR "$PROGRAMFILES64\Block"
  init_dir_ready:

  ; In-app updates (/UPDATE) always run completely silently with zero UI.
  StrCmp $BLOCK_IS_UPDATE "1" 0 init_check_silent
    SetSilent silent
    Goto init_done

  init_check_silent:
  IfSilent init_done

  InitPluginsDir
  ClearErrors
  File "/oname=$PLUGINSDIR\block-setup.exe" "${BINARY}"
  IfErrors init_done

  ; Values travel through the environment: an install path ending in "\" or
  ; holding quotes cannot break the child's command line that way.
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_EXE", t "$EXEPATH")'
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_DIR", t "$INSTDIR")'
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_UPDATE", t "$BLOCK_IS_UPDATE")'
  System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_SETUP_ALLUSERS", t "$BLOCK_FORCE_ALLUSERS")'

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
  StrCpy $BLOCK_FAIL_REASON "$(INST_FAIL_GENERIC)"
  !insertmacro BLOCK_CHECK_ADMIN inst_adm
  !insertmacro BLOCK_COMPUTE_SCOPE_HASH inst_hash
  !insertmacro BLOCK_DETECT_INSTALL_SCOPE inst_scope

  StrCmp $BLOCK_INSTALL_SCOPE "machine" inst_ctx_all inst_ctx_user
  inst_ctx_all:
    StrCmp $BLOCK_IS_ADMIN "1" inst_verify_machine_dir 0
      StrCpy $BLOCK_FAIL_REASON "$(INST_FAIL_ADMIN)"
      Goto inst_fail_have_exe
    inst_verify_machine_dir:
      ; Reject machine-wide installs into directories modifiable or hijackable
      ; via junctions/symlinks by non-administrator accounts before touching disk.
      InitPluginsDir
      ClearErrors
      File "/oname=$PLUGINSDIR\block-verify.exe" "${BINARY}"
      IfErrors inst_acl_check_failed
      System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_VERIFY_MACHINE_DIR", t "$INSTDIR")'
      StrCpy $R9 "1"
      ClearErrors
      ExecWait '"$PLUGINSDIR\block-verify.exe" --installer-ui' $R9
      System::Call 'kernel32::SetEnvironmentVariable(t "BLOCK_VERIFY_MACHINE_DIR", p 0)'
      IfErrors inst_acl_check_failed
      StrCmp $R9 "0" inst_acl_ok inst_acl_check_failed
    inst_acl_check_failed:
      ; Do not touch $INSTDIR or launch anything from an untrusted directory.
      StrCpy $BLOCK_FAIL_REASON "$(INST_FAIL_UNSAFE_DIR)"
      StrCmp $BLOCK_IS_UPDATE "1" 0 inst_fail_abort
        WriteRegStr HKCU "Software\Block" "LastUpdateError" "$BLOCK_FAIL_REASON"
      Goto inst_fail_abort
    inst_acl_ok:
    SetShellVarContext all
    Goto inst_ctx_done
  inst_ctx_user:
    SetShellVarContext current
  inst_ctx_done:

  !insertmacro BLOCK_COMPUTE_SCOPE_IDENTITY inst_id
  !insertmacro BLOCK_WAIT_EXE_UNLOCKED inst_wait

  ; Cancel any deferred per-user cleanup scheduled by a prior machine-wide
  ; uninstall of this exact directory before laying down files and registry keys.
  StrCmp $BLOCK_INSTALL_SCOPE "machine" 0 inst_skip_cancel_cleanup
    DeleteRegValue HKLM "Software\Block\PendingCleanup\$BLOCK_SCOPE_ID" "ExecutablePath"
    DeleteRegKey HKLM "Software\Microsoft\Active Setup\Installed Components\Block.Cleanup.$BLOCK_SCOPE_ID"
    Delete "$APPDATA\Block\cleanup-$BLOCK_SCOPE_ID.ps1"
  inst_skip_cancel_cleanup:

  ClearErrors
  SetOutPath $INSTDIR
  IfErrors inst_write_failed

  ; Stage the new binary on the same volume first, then swap atomically so an
  ; interruption mid-unpack never leaves a truncated block.exe behind.
  Delete "$INSTDIR\block.exe.new"
  ClearErrors
  File "/oname=$INSTDIR\block.exe.new" "${BINARY}"
  IfErrors inst_write_failed
  IfFileExists "$INSTDIR\block.exe.new" 0 inst_write_failed

  Delete "$INSTDIR\block.exe.old"
  IfFileExists "$INSTDIR\block.exe" inst_replace_existing inst_move_new
  inst_replace_existing:
    ; REPLACEFILE_IGNORE_MERGE_ERRORS (2) | REPLACEFILE_IGNORE_ACL_ERRORS (4) = 6
    System::Call 'kernel32::ReplaceFileW(t "$INSTDIR\block.exe", t "$INSTDIR\block.exe.new", t "$INSTDIR\block.exe.old", i 6, p 0, p 0) i .r0'
    IntCmp $0 0 inst_write_failed inst_exe_ready inst_exe_ready
  inst_move_new:
    ; MOVEFILE_REPLACE_EXISTING = 1
    System::Call 'kernel32::MoveFileExW(t "$INSTDIR\block.exe.new", t "$INSTDIR\block.exe", i 1) i .r0'
    IntCmp $0 0 inst_write_failed inst_exe_ready inst_exe_ready
  inst_exe_ready:

  CreateShortcut "$BLOCK_SHORTCUT_PATH" "$INSTDIR\block.exe" "" "$INSTDIR\block.exe" 0
  ClearErrors
  WriteUninstaller "$INSTDIR\uninstall.exe"
  IfErrors inst_write_failed

  StrCmp $BLOCK_INSTALL_SCOPE "machine" inst_reg_machine inst_reg_user

  inst_reg_machine:
    ; Migrate legacy shared Uninstall\Block entry ONLY if it belonged to this exact $INSTDIR.
    ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
    StrCmp $0 "$INSTDIR" 0 inst_skip_legacy_uninst
      DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block"
    inst_skip_legacy_uninst:

    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "DisplayName" "$BLOCK_DISPLAY_NAME"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "DisplayVersion" "${VERSION}"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "Publisher" "Block"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "DisplayIcon" "$INSTDIR\block.exe,0"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockScopeId" "$BLOCK_SCOPE_ID"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockInstallScope" "machine"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockMachineProgID" "$BLOCK_MACHINE_PROGID"
    WriteRegStr HKLM "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath" "$BLOCK_SHORTCUT_PATH"
    WriteRegDWORD HKLM "$BLOCK_UNINSTALL_KEY" "NoModify" 1
    WriteRegDWORD HKLM "$BLOCK_UNINSTALL_KEY" "NoRepair" 1
    Goto inst_reg_done

  inst_reg_user:
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "DisplayName" "$BLOCK_DISPLAY_NAME"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "Publisher" "Block"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "DisplayIcon" "$INSTDIR\block.exe,0"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "BlockScopeId" "$BLOCK_SCOPE_ID"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "BlockInstallScope" "user"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "BlockUserProgID" "$BLOCK_USER_PROGID"
    WriteRegStr HKCU "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath" "$BLOCK_SHORTCUT_PATH"
    WriteRegDWORD HKCU "$BLOCK_UNINSTALL_KEY" "NoModify" 1
    WriteRegDWORD HKCU "$BLOCK_UNINSTALL_KEY" "NoRepair" 1

  inst_reg_done:
  !include "${ASSOC_NSH}"

  ; Seamless in-app updates automatically reopen Block once installation completes.
  StrCmp $BLOCK_IS_UPDATE "1" 0 inst_cleanup_backup
    DeleteRegValue HKCU "Software\Block" "LastUpdateError"
    ClearErrors
    StrCmp $BLOCK_IS_ADMIN "1" inst_relaunch_via_shell inst_relaunch_direct
    inst_relaunch_via_shell:
      ; Avoid handing administrator elevation to Block when updating a machine install.
      Exec '"$WINDIR\explorer.exe" "$INSTDIR\block.exe"'
      IfErrors inst_relaunch_failed inst_cleanup_backup
    inst_relaunch_direct:
      Exec '"$INSTDIR\block.exe"'
      IfErrors inst_relaunch_failed inst_cleanup_backup
  inst_relaunch_failed:
    IfFileExists "$INSTDIR\block.exe.old" 0 inst_relaunch_fail_msg
      System::Call 'kernel32::MoveFileExW(t "$INSTDIR\block.exe.old", t "$INSTDIR\block.exe", i 1) i .r0'
    inst_relaunch_fail_msg:
      StrCpy $BLOCK_FAIL_REASON "$(INST_FAIL_RELAUNCH)"
      Goto inst_write_failed

  inst_cleanup_backup:
    Delete "$INSTDIR\block.exe.old"
    Goto inst_done

  inst_write_failed:
    Delete "$INSTDIR\block.exe.new"
    IfFileExists "$INSTDIR\block.exe.old" 0 inst_fail_have_exe
      System::Call 'kernel32::MoveFileExW(t "$INSTDIR\block.exe.old", t "$INSTDIR\block.exe", i 1) i .r0'
    inst_fail_have_exe:
    StrCmp $BLOCK_IS_UPDATE "1" 0 inst_fail_abort
      WriteRegStr HKCU "Software\Block" "LastUpdateError" "$BLOCK_FAIL_REASON"
      IfFileExists "$INSTDIR\block.exe" 0 inst_fail_abort
        StrCmp $BLOCK_IS_ADMIN "1" inst_fail_relaunch_shell inst_fail_relaunch_direct
        inst_fail_relaunch_shell:
          Exec '"$WINDIR\explorer.exe" "$INSTDIR\block.exe"'
          Goto inst_fail_abort
        inst_fail_relaunch_direct:
          Exec '"$INSTDIR\block.exe"'
    inst_fail_abort:
      SetErrorLevel 1
      Abort
  inst_done:
SectionEnd

Function un.onInit
  !insertmacro BLOCK_SELECT_PREFERRED_LANGUAGE un_init_lang
  !insertmacro BLOCK_CHECK_ADMIN un_init_adm
  !insertmacro BLOCK_COMPUTE_SCOPE_HASH un_init_hash
  StrCpy $BLOCK_FORCE_ALLUSERS ""
  !insertmacro BLOCK_DETECT_INSTALL_SCOPE un_init_scope

  ; If uninstalling a machine-wide installation without admin privileges,
  ; self-elevate using the installed uninstall.exe so UAC is requested cleanly.
  StrCmp $BLOCK_INSTALL_SCOPE "machine" 0 un_init_done
  StrCmp $BLOCK_IS_ADMIN "1" un_init_done 0

  ${un.GetParameters} $R0
  ClearErrors
  ${un.GetOptions} "$R0" "/ELEVATED" $R1
  IfErrors 0 un_init_done
  ClearErrors

  StrCpy $R4 "/ELEVATED"
  IfSilent 0 un_init_elev_exec
    StrCpy $R4 "/S /ELEVATED"
  un_init_elev_exec:
  IfFileExists "$INSTDIR\uninstall.exe" 0 un_init_done
  System::Call 'shell32::ShellExecuteW(p 0, t "runas", t "$INSTDIR\uninstall.exe", t "$R4", t "$INSTDIR", i 1) p .r5'
  SetErrorLevel 0
  Quit
  un_init_done:
FunctionEnd

Section "Uninstall"
  !insertmacro BLOCK_COMPUTE_SCOPE_HASH un_hash
  StrCpy $BLOCK_FORCE_ALLUSERS ""
  !insertmacro BLOCK_DETECT_INSTALL_SCOPE un_scope

  StrCmp $BLOCK_INSTALL_SCOPE "machine" un_ctx_all un_ctx_user
  un_ctx_all:
    SetShellVarContext all
    Goto un_ctx_done
  un_ctx_user:
    SetShellVarContext current
  un_ctx_done:

  !insertmacro BLOCK_COMPUTE_SCOPE_IDENTITY un_id

  StrCmp $BLOCK_INSTALL_SCOPE "machine" un_do_machine un_do_user

  un_do_user:
    ReadRegStr $R0 HKCU "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath"
    StrCmp $R0 "" 0 un_user_have_shortcut
      StrCpy $R0 "$BLOCK_SHORTCUT_PATH"
    un_user_have_shortcut:

    Delete "$INSTDIR\block.exe.new"
    Delete "$INSTDIR\block.exe.old"
    IfFileExists "$INSTDIR\block.exe" 0 un_user_exe_removed
      !insertmacro BLOCK_WAIT_EXE_UNLOCKED_SHORT un_user_wait
      ClearErrors
      Delete "$INSTDIR\block.exe"
      IfFileExists "$INSTDIR\block.exe" un_exe_in_use 0
    un_user_exe_removed:

    Delete "$INSTDIR\uninstall.exe"
    Delete "$R0"
    RMDir "$INSTDIR"

    ReadRegStr $0 HKCU "$BLOCK_UNINSTALL_KEY" "InstallLocation"
    StrCmp $0 "" un_del_user_uninst
    StrCmp $0 "$INSTDIR" un_del_user_uninst 0
    StrCmp $0 "$BLOCK_NORM_DIR" un_del_user_uninst un_keep_user_uninst
    un_del_user_uninst:
      DeleteRegKey HKCU "$BLOCK_UNINSTALL_KEY"
    un_keep_user_uninst:

    DeleteRegKey HKCU "Software\Block\UserAssoc\$BLOCK_SCOPE_ID"
    !insertmacro UN_CLEAN_HKCU_CLASSES_ROOT un_hkcu_cls
    System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
    Goto un_done

  un_do_machine:
    ; Resolve recorded shortcut and machine ProgID before removing uninstall metadata.
    ReadRegStr $R0 HKLM "$BLOCK_UNINSTALL_KEY" "BlockShortcutPath"
    StrCmp $R0 "" 0 un_have_shortcut
      StrCpy $R0 "$BLOCK_SHORTCUT_PATH"
    un_have_shortcut:
    ReadRegStr $R1 HKLM "$BLOCK_UNINSTALL_KEY" "BlockMachineProgID"
    StrCmp $R1 "" 0 un_have_progid
      StrCpy $R1 "$BLOCK_MACHINE_PROGID"
    un_have_progid:

    Delete "$INSTDIR\block.exe.new"
    Delete "$INSTDIR\block.exe.old"
    IfFileExists "$INSTDIR\block.exe" 0 un_machine_exe_removed
      !insertmacro BLOCK_WAIT_EXE_UNLOCKED_SHORT un_machine_wait
      ClearErrors
      Delete "$INSTDIR\block.exe"
      IfFileExists "$INSTDIR\block.exe" un_exe_in_use 0
    un_machine_exe_removed:

    Delete "$INSTDIR\uninstall.exe"
    Delete "$R0"
    RMDir "$INSTDIR"

    ; Remove only this installation's uninstall registration (and legacy shared
    ; Uninstall\Block ONLY if its InstallLocation matches this exact $INSTDIR).
    ReadRegStr $0 HKLM "$BLOCK_UNINSTALL_KEY" "InstallLocation"
    StrCmp $0 "" un_del_scoped_uninst
    StrCmp $0 "$INSTDIR" un_del_scoped_uninst 0
    StrCmp $0 "$BLOCK_NORM_DIR" un_del_scoped_uninst un_keep_scoped_uninst
    un_del_scoped_uninst:
      DeleteRegKey HKLM "$BLOCK_UNINSTALL_KEY"
    un_keep_scoped_uninst:

    ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
    StrCmp $0 "$INSTDIR" un_del_legacy_uninst 0
    StrCmp $0 "$BLOCK_NORM_DIR" un_del_legacy_uninst un_keep_legacy_uninst
    un_del_legacy_uninst:
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
    Goto un_done

  un_exe_in_use:
    IfSilent un_exe_in_use_abort
      MessageBox MB_ICONEXCLAMATION|MB_OK "$(UN_EXE_IN_USE)"
    un_exe_in_use_abort:
      SetErrorLevel 1
      Abort

  un_done:
SectionEnd
