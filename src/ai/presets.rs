//! Built-in AI provider presets (modeled on Cherry Studio) and protocol definitions.
use crate::theme::tr;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "lowercase")]
pub(crate) enum Protocol {
    #[default]
    Openai,
    Anthropic,
}

impl Protocol {
    pub(crate) fn label(self) -> &'static str {
        match self {
            Protocol::Openai => tr("OpenAI 兼容", "OpenAI Compatible"),
            Protocol::Anthropic => tr("Anthropic 兼容", "Anthropic Compatible"),
        }
    }
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct Preset {
    pub id: &'static str,
    pub zh: &'static str,
    pub en: &'static str,
    pub protocol: Protocol,
    pub base_url: &'static str,
    pub key_required: bool,
}

impl Preset {
    pub(crate) fn label(&self) -> &'static str {
        tr(self.zh, self.en)
    }
}

#[derive(Clone, Debug, Default, serde::Deserialize, serde::Serialize)]
pub(crate) struct ProviderProfile {
    #[serde(default)]
    pub protocol: Protocol,
    #[serde(default)]
    pub base_url: String,
    #[serde(default)]
    pub api_key: String,
    #[serde(default)]
    pub model: String,
}

pub(crate) const PRESETS: &[Preset] = &[
    Preset {
        id: "openai",
        zh: "OpenAI",
        en: "OpenAI",
        protocol: Protocol::Openai,
        base_url: "https://api.openai.com/v1",
        key_required: true,
    },
    Preset {
        id: "anthropic",
        zh: "Anthropic",
        en: "Anthropic",
        protocol: Protocol::Anthropic,
        base_url: "https://api.anthropic.com/v1",
        key_required: true,
    },
    Preset {
        id: "gemini",
        zh: "Google Gemini",
        en: "Google Gemini",
        protocol: Protocol::Openai,
        base_url: "https://generativelanguage.googleapis.com/v1beta/openai",
        key_required: true,
    },
    Preset {
        id: "deepseek",
        zh: "DeepSeek",
        en: "DeepSeek",
        protocol: Protocol::Openai,
        base_url: "https://api.deepseek.com/v1",
        key_required: true,
    },
    Preset {
        id: "openrouter",
        zh: "OpenRouter",
        en: "OpenRouter",
        protocol: Protocol::Openai,
        base_url: "https://openrouter.ai/api/v1",
        key_required: true,
    },
    Preset {
        id: "siliconflow",
        zh: "硅基流动 SiliconFlow",
        en: "SiliconFlow",
        protocol: Protocol::Openai,
        base_url: "https://api.siliconflow.cn/v1",
        key_required: true,
    },
    Preset {
        id: "moonshot",
        zh: "月之暗面 Moonshot",
        en: "Moonshot",
        protocol: Protocol::Openai,
        base_url: "https://api.moonshot.cn/v1",
        key_required: true,
    },
    Preset {
        id: "zhipu",
        zh: "智谱 Zhipu",
        en: "Zhipu",
        protocol: Protocol::Openai,
        base_url: "https://open.bigmodel.cn/api/paas/v4",
        key_required: true,
    },
    Preset {
        id: "dashscope",
        zh: "通义千问 DashScope",
        en: "DashScope",
        protocol: Protocol::Openai,
        base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1",
        key_required: true,
    },
    Preset {
        id: "ollama",
        zh: "Ollama",
        en: "Ollama",
        protocol: Protocol::Openai,
        base_url: "http://localhost:11434/v1",
        key_required: false,
    },
    Preset {
        id: "lmstudio",
        zh: "LM Studio",
        en: "LM Studio",
        protocol: Protocol::Openai,
        base_url: "http://localhost:1234/v1",
        key_required: false,
    },
    Preset {
        id: "custom",
        zh: "自定义",
        en: "Custom",
        protocol: Protocol::Openai,
        base_url: "",
        key_required: true,
    },
];

pub(crate) fn find(id: &str) -> Option<&'static Preset> {
    PRESETS.iter().find(|p| p.id == id)
}

pub(crate) fn infer_from_base_url(base_url: &str) -> Option<&'static Preset> {
    let trimmed = base_url.trim().trim_end_matches('/');
    if trimmed.is_empty() {
        return None;
    }
    PRESETS
        .iter()
        .find(|p| !p.base_url.is_empty() && p.base_url.trim_end_matches('/') == trimmed)
}
