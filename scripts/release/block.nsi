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
SectionEnd
