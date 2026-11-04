//! Built-in AI provider presets (modeled on Cherry Studio) and protocol definitions.
use crate::theme::tr;

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, serde::Deserialize, serde::Serialize)]
#[serde(rename_all = "lowercase")]
pub(crate) enum Protocol {
    #[default]
    Openai,
    Anthropic,
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct Preset {
    pub id: &'static str,
    pub zh: &'static str,
    pub en: &'static str,
    pub hint: &'static str,
    pub protocol: Protocol,
    pub base_url: &'static str,
    pub brand_rgb: u32,
    pub key_required: bool,
}

impl Preset {
    pub(crate) fn label(&self) -> &'static str {
        tr(self.zh, self.en)
    }

    pub(crate) fn icon(&self) -> crate::icons::Icon {
        use crate::icons::Icon;
        match self.id {
            "openai" => Icon::ProvOpenai,
            "anthropic" => Icon::ProvAnthropic,
            _ => Icon::ProvDeepseek,
        }
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
        id: "deepseek", zh: "DeepSeek", en: "DeepSeek", hint: "api.deepseek.com",
        protocol: Protocol::Openai, base_url: "https://api.deepseek.com/v1", brand_rgb: 0x4d6bfe, key_required: true,
    },
    Preset {
        id: "openai", zh: "OpenAI 协议", en: "OpenAI Protocol", hint: "/v1/chat/completions",
        protocol: Protocol::Openai, base_url: "https://api.openai.com/v1", brand_rgb: 0x10a37f, key_required: true,
    },
    Preset {
        id: "anthropic", zh: "Claude 协议", en: "Claude Protocol", hint: "/v1/messages",
        protocol: Protocol::Anthropic, base_url: "https://api.anthropic.com/v1", brand_rgb: 0xd97757, key_required: true,
    },
];

/// Maps any stored provider (including retired presets) onto one of the three cards.
pub(crate) fn normalize_provider_id(id: &str, protocol: Protocol, base_url: &str) -> &'static str {
    if id == "deepseek" || base_url.contains("deepseek") { return "deepseek"; }
    if id == "anthropic" || id == "claude" || protocol == Protocol::Anthropic || base_url.contains("anthropic") {
        return "anthropic";
    }
    if id.trim().is_empty() && base_url.trim().is_empty() { return "deepseek"; }
    "openai"
}

pub(crate) fn find(id: &str) -> Option<&'static Preset> {
    let id = if id == "claude" { "anthropic" } else { id };
    PRESETS.iter().find(|p| p.id == id)
}
