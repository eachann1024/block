package installer

import (
	"os"
	"strings"

	"block/internal/model"
	"block/internal/theme"
)

type stringsTable struct {
	tag                  string
	appLang              model.LanguageMode
	title                string
	minimize             string
	close                string
	install              string
	installing           string
	updating             string
	openBlock            string
	retry                string
	changeLocation       string
	chooseDirTitle       string
	selectBtn            string
	errAppRunning        string
	errInvalidDir        string
	errUnsafeMachineDir  string
	errElevationCanceled string
	errGeneric           string
}

var localeCatalog = map[string]stringsTable{
	"en": {
		tag:                  "en",
		appLang:              model.LangEn,
		title:                "Block Setup",
		minimize:             "Minimize",
		close:                "Close",
		install:              "Install",
		installing:           "Installing…",
		updating:             "Updating…",
		openBlock:            "Open Block",
		retry:                "Retry",
		changeLocation:       "Change Location",
		chooseDirTitle:       "Choose Install Location",
		selectBtn:            "Select",
		errAppRunning:        "Close the running Block, then try again.",
		errInvalidDir:        "Block can't be installed here. Choose another folder.",
		errUnsafeMachineDir:  "System-wide installs require an administrator-protected folder (such as Program Files). Choose another location.",
		errElevationCanceled: "Administrator permission was canceled. Try again or change location.",
		errGeneric:           "Setup didn't finish. Try again.",
	},
	"zh-Hans": {
		tag:                  "zh-Hans",
		appLang:              model.LangZh,
		title:                "安装 Block",
		minimize:             "最小化",
		close:                "关闭",
		install:              "安装",
		installing:           "正在安装…",
		updating:             "正在更新…",
		openBlock:            "打开 Block",
		retry:                "重试",
		changeLocation:       "更改位置",
		chooseDirTitle:       "选择安装位置",
		selectBtn:            "选择",
		errAppRunning:        "请先关闭正在运行的 Block，再重试。",
		errInvalidDir:        "这个位置无法安装，请换一个文件夹。",
		errUnsafeMachineDir:  "全局安装必须选择受管理员保护的目录（如 Program Files），请更换位置。",
		errElevationCanceled: "已取消管理员授权，可重试或更改位置。",
		errGeneric:           "安装没有完成，请重试。",
	},
	"zh-Hant": {
		tag:                  "zh-Hant",
		appLang:              model.LangZh,
		title:                "安裝 Block",
		minimize:             "最小化",
		close:                "關閉",
		install:              "安裝",
		installing:           "正在安裝…",
		updating:             "正在更新…",
		openBlock:            "開啟 Block",
		retry:                "重試",
		changeLocation:       "更改位置",
		chooseDirTitle:       "選擇安裝位置",
		selectBtn:            "選擇",
		errAppRunning:        "請先關閉正在執行的 Block，再重試。",
		errInvalidDir:        "這個位置無法安裝，請換一個資料夾。",
		errUnsafeMachineDir:  "全域安裝必須選擇受系統管理員保護的目錄（如 Program Files），請更換位置。",
		errElevationCanceled: "已取消系統管理員授權，可重試或更改位置。",
		errGeneric:           "安裝尚未完成，請重試。",
	},
	"ja": {
		tag:                  "ja",
		appLang:              model.LangEn,
		title:                "Block のインストール",
		minimize:             "最小化",
		close:                "閉じる",
		install:              "インストール",
		installing:           "インストール中…",
		updating:             "アップデート中…",
		openBlock:            "Block を開く",
		retry:                "再試行",
		changeLocation:       "場所を変更",
		chooseDirTitle:       "インストール先を選択",
		selectBtn:            "選択",
		errAppRunning:        "実行中の Block を終了してから再試行してください。",
		errInvalidDir:        "この場所にはインストールできません。別のフォルダーを選択してください。",
		errUnsafeMachineDir:  "全ユーザー向けインストールには管理者保護されたフォルダー（Program Files など）が必要です。別の場所を選択してください。",
		errElevationCanceled: "管理者権限の許可がキャンセルされました。再試行するか場所を変更してください。",
		errGeneric:           "インストールを完了できませんでした。再試行してください。",
	},
	"ko": {
		tag:                  "ko",
		appLang:              model.LangEn,
		title:                "Block 설치",
		minimize:             "최소화",
		close:                "닫기",
		install:              "설치",
		installing:           "설치 중…",
		updating:             "업데이트 중…",
		openBlock:            "Block 열기",
		retry:                "다시 시도",
		changeLocation:       "위치 변경",
		chooseDirTitle:       "설치 위치 선택",
		selectBtn:            "선택",
		errAppRunning:        "실행 중인 Block을 종료한 후 다시 시도하세요.",
		errInvalidDir:        "이 위치에는 설치할 수 없습니다. 다른 폴더를 선택하세요.",
		errUnsafeMachineDir:  "시스템 전체 설치에는 관리자 보호 폴더(예: Program Files)가 필요합니다. 다른 위치를 선택하세요.",
		errElevationCanceled: "관리자 권한 승인이 취소되었습니다. 다시 시도하거나 위치를 변경하세요.",
		errGeneric:           "설치를 완료하지 못했습니다. 다시 시도하세요.",
	},
	"fr": {
		tag:                  "fr",
		appLang:              model.LangEn,
		title:                "Installation de Block",
		minimize:             "Réduire",
		close:                "Fermer",
		install:              "Installer",
		installing:           "Installation…",
		updating:             "Mise à jour…",
		openBlock:            "Ouvrir Block",
		retry:                "Réessayer",
		changeLocation:       "Changer l’emplacement",
		chooseDirTitle:       "Choisir le dossier d’installation",
		selectBtn:            "Sélectionner",
		errAppRunning:        "Fermez Block avant de réessayer.",
		errInvalidDir:        "Impossible d’installer Block ici. Choisissez un autre dossier.",
		errUnsafeMachineDir:  "L’installation globale nécessite un dossier protégé par administrateur (tel que Program Files). Choisissez un autre emplacement.",
		errElevationCanceled: "L’autorisation administrateur a été annulée. Réessayez ou changez d’emplacement.",
		errGeneric:           "L’installation n’a pas abouti. Réessayez.",
	},
	"de": {
		tag:                  "de",
		appLang:              model.LangEn,
		title:                "Block-Installation",
		minimize:             "Minimieren",
		close:                "Schließen",
		install:              "Installieren",
		installing:           "Wird installiert…",
		updating:             "Wird aktualisiert…",
		openBlock:            "Block öffnen",
		retry:                "Erneut versuchen",
		changeLocation:       "Ordner ändern",
		chooseDirTitle:       "Installationsordner wählen",
		selectBtn:            "Auswählen",
		errAppRunning:        "Schließen Sie Block und versuchen Sie es erneut.",
		errInvalidDir:        "Block kann hier nicht installiert werden. Wählen Sie einen anderen Ordner.",
		errUnsafeMachineDir:  "Systemweite Installationen erfordern einen administratorgeschützten Ordner (z. B. Program Files). Wählen Sie einen anderen Ort.",
		errElevationCanceled: "Administratorberechtigung abgebrochen. Versuchen Sie es erneut oder wählen Sie einen anderen Ort.",
		errGeneric:           "Die Installation wurde nicht abgeschlossen. Versuchen Sie es erneut.",
	},
	"es": {
		tag:                  "es",
		appLang:              model.LangEn,
		title:                "Instalación de Block",
		minimize:             "Minimizar",
		close:                "Cerrar",
		install:              "Instalar",
		installing:           "Instalando…",
		updating:             "Actualizando…",
		openBlock:            "Abrir Block",
		retry:                "Reintentar",
		changeLocation:       "Cambiar ubicación",
		chooseDirTitle:       "Elegir ubicación de instalación",
		selectBtn:            "Seleccionar",
		errAppRunning:        "Cierra Block y vuelve a intentarlo.",
		errInvalidDir:        "Block no se puede instalar aquí. Elige otra carpeta.",
		errUnsafeMachineDir:  "La instalación para todo el sistema requiere una carpeta protegida por administrador (como Program Files). Elige otra ubicación.",
		errElevationCanceled: "Se canceló el permiso de administrador. Reintenta o cambia la ubicación.",
		errGeneric:           "La instalación no se completó. Inténtalo de nuevo.",
	},
	"pt": {
		tag:                  "pt",
		appLang:              model.LangEn,
		title:                "Instalação do Block",
		minimize:             "Minimizar",
		close:                "Fechar",
		install:              "Instalar",
		installing:           "Instalando…",
		updating:             "Atualizando…",
		openBlock:            "Abrir Block",
		retry:                "Tentar novamente",
		changeLocation:       "Alterar local",
		chooseDirTitle:       "Escolher local de instalação",
		selectBtn:            "Selecionar",
		errAppRunning:        "Feche o Block em execução e tente novamente.",
		errInvalidDir:        "Não é possível instalar o Block aqui. Escolha outra pasta.",
		errUnsafeMachineDir:  "A instalação para todo o sistema requer uma pasta protegida por administrador (como Arquivos de Programas). Escolha outro local.",
		errElevationCanceled: "A permissão de administrador foi cancelada. Tente novamente ou altere o local.",
		errGeneric:           "A instalação não foi concluída. Tente novamente.",
	},
	"ru": {
		tag:                  "ru",
		appLang:              model.LangEn,
		title:                "Установка Block",
		minimize:             "Свернуть",
		close:                "Закрыть",
		install:              "Установить",
		installing:           "Установка…",
		updating:             "Обновление…",
		openBlock:            "Открыть Block",
		retry:                "Повторить",
		changeLocation:       "Изменить папку",
		chooseDirTitle:       "Выберите папку для установки",
		selectBtn:            "Выбрать",
		errAppRunning:        "Закройте запущенный Block и повторите попытку.",
		errInvalidDir:        "Сюда нельзя установить Block. Выберите другую папку.",
		errUnsafeMachineDir:  "Для установки для всех пользователей требуется защищённая администратором папка (например, Program Files). Выберите другое место.",
		errElevationCanceled: "Запрос прав администратора отменён. Повторите попытку или измените папку.",
		errGeneric:           "Установка не завершена. Повторите попытку.",
	},
	"it": {
		tag:                  "it",
		appLang:              model.LangEn,
		title:                "Installazione di Block",
		minimize:             "Riduci a icona",
		close:                "Chiudi",
		install:              "Installa",
		installing:           "Installazione…",
		updating:             "Aggiornamento…",
		openBlock:            "Apri Block",
		retry:                "Riprova",
		changeLocation:       "Cambia percorso",
		chooseDirTitle:       "Scegli percorso di installazione",
		selectBtn:            "Seleziona",
		errAppRunning:        "Chiudi Block e riprova.",
		errInvalidDir:        "Impossibile installare Block qui. Scegli un’altra cartella.",
		errUnsafeMachineDir:  "L’installazione di sistema richiede una cartella protetta da amministratore (come Program Files). Scegli un altro percorso.",
		errElevationCanceled: "Autorizzazione amministratore annullata. Riprova o cambia percorso.",
		errGeneric:           "Installazione non completata. Riprova.",
	},
}

// resolveStrings picks the installer copy matching the user's preferred system
// languages, falling back to English when no supported locale matches.
func resolveStrings(override string) stringsTable {
	candidates := make([]string, 0, 8)
	for _, raw := range []string{override, os.Getenv("BLOCK_SETUP_LANG"), os.Getenv("MYGO_LOCALE")} {
		if t := strings.TrimSpace(raw); t != "" {
			candidates = append(candidates, t)
		}
	}
	candidates = append(candidates, theme.SystemPreferredLanguages()...)
	for _, raw := range candidates {
		if tbl, ok := matchLocaleTable(raw); ok {
			return tbl
		}
	}
	return localeCatalog["en"]
}

func matchLocaleTable(raw string) (stringsTable, bool) {
	tag := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, "_", "-")))
	if tag == "" {
		return stringsTable{}, false
	}
	parts := strings.Split(tag, "-")
	primary := parts[0]
	if primary == "zh" {
		for _, sub := range parts[1:] {
			if sub == "hant" || sub == "tw" || sub == "hk" || sub == "mo" {
				return localeCatalog["zh-Hant"], true
			}
		}
		return localeCatalog["zh-Hans"], true
	}
	if tbl, ok := localeCatalog[primary]; ok {
		return tbl, true
	}
	return stringsTable{}, false
}
