Unicode true
!include "MUI2.nsh"

!ifndef VERSION
  !error "VERSION is required"
!endif
!ifndef OUTFILE
  !error "OUTFILE is required"
!endif
!ifndef BINARY
  !error "BINARY is required"
!endif

Name "Block"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\Block"
InstallDirRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation"
RequestExecutionLevel admin
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "Block"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileDescription" "Block installer"

!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath $INSTDIR
  File /oname=block.exe "${BINARY}"
  CreateShortcut "$SMPROGRAMS\Block.lnk" "$INSTDIR\block.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "DisplayName" "Block"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "Publisher" "Block"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block" "NoRepair" 1
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\block.exe"
  Delete "$INSTDIR\uninstall.exe"
  Delete "$SMPROGRAMS\Block.lnk"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\Block"
  ; Clean Block's per-user ProgID and OpenWithProgids across all loaded user
  ; hives (HKU\<SID>_Classes and HKU\<SID>\Software\Classes) whose ProgID points
  ; to this uninstalled $INSTDIR\block.exe. Handles standard-user UAC elevation
  ; via a separate admin account without executing any installed binary.
  StrCpy $0 0
  un_user_loop:
    EnumRegKey $1 HKU "" $0
    StrCmp $1 "" un_user_done
    IntOp $0 $0 + 1
    ReadRegStr $2 HKU "$1\Block.Document\shell\open\command" ""
    StrCmp $2 '"$INSTDIR\block.exe" "%1"' 0 un_user_check_sw
    DeleteRegKey HKU "$1\Block.Document"
    StrCpy $3 0
    un_classes_ext_loop:
      EnumRegKey $4 HKU "$1" $3
      StrCmp $4 "" un_user_check_sw
      IntOp $3 $3 + 1
      StrCpy $5 $4 1
      StrCmp $5 "." 0 un_classes_ext_loop
      DeleteRegValue HKU "$1\$4\OpenWithProgids" "Block.Document"
      Goto un_classes_ext_loop
  un_user_check_sw:
    ReadRegStr $2 HKU "$1\Software\Classes\Block.Document\shell\open\command" ""
    StrCmp $2 '"$INSTDIR\block.exe" "%1"' 0 un_user_loop
    DeleteRegKey HKU "$1\Software\Classes\Block.Document"
    StrCpy $3 0
    un_sw_ext_loop:
      EnumRegKey $4 HKU "$1\Software\Classes" $3
      StrCmp $4 "" un_user_loop
      IntOp $3 $3 + 1
      StrCpy $5 $4 1
      StrCmp $5 "." 0 un_sw_ext_loop
      DeleteRegValue HKU "$1\Software\Classes\$4\OpenWithProgids" "Block.Document"
      Goto un_sw_ext_loop
  un_user_done:
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd
