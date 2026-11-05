//! Token-stack based word-pair matching for nested blocks across Bash, Lean, SQL, Mermaid, and LaTeX.
use std::ops::Range;
use super::code_selection_tokens::{CodeToken, TokenKind};

#[derive(Debug, Clone)]
pub struct WordPair {
    pub open_range: Range<usize>,
    pub close_range: Range<usize>,
}

pub fn match_word_pair_block(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    lang: &str,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    if lang == "latex" {
        return match_latex_env(text, tokens, offset, include_delimiters);
    }

    let pairs = collect_word_pairs(text, tokens, lang);
    let matched = pairs.into_iter().filter(|p| {
        let at_open = offset >= p.open_range.start && offset <= p.open_range.end;
        let at_close = offset >= p.close_range.start && offset <= p.close_range.end;
        at_open || at_close
    }).min_by_key(|p| p.close_range.end.saturating_sub(p.open_range.start))?;

    if include_delimiters {
        Some(matched.open_range.start..matched.close_range.end)
    } else {
        let inner_start = matched.open_range.end;
        let inner_end = matched.close_range.start;
        Some(trim_span(text, inner_start..inner_end))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum ExpectedClose {
    Exact(&'static str),
    CaseInsensitive(&'static str),
}

impl ExpectedClose {
    fn matches(&self, word: &str) -> bool {
        match self {
            ExpectedClose::Exact(expected) => word == *expected,
            ExpectedClose::CaseInsensitive(expected) => word.eq_ignore_ascii_case(expected),
        }
    }
}

struct WordFrame {
    expected_close: ExpectedClose,
    open_range: Range<usize>,
}

fn collect_word_pairs(text: &str, tokens: &[CodeToken], lang: &str) -> Vec<WordPair> {
    let mut pairs = Vec::new();
    let mut stack: Vec<WordFrame> = Vec::new();

    for t in tokens {
        if !matches!(t.kind, TokenKind::Word) { continue; }
        if t.range.end > text.len() { continue; }
        let word = &text[t.range.clone()];

        match lang {
            "bash" => match word {
                "if" => stack.push(WordFrame { expected_close: ExpectedClose::Exact("fi"), open_range: t.range.clone() }),
                "case" => stack.push(WordFrame { expected_close: ExpectedClose::Exact("esac"), open_range: t.range.clone() }),
                "for" | "while" | "until" | "select" => stack.push(WordFrame { expected_close: ExpectedClose::Exact("done"), open_range: t.range.clone() }),
                "fi" | "esac" | "done" => {
                    pop_matching_word(&mut stack, &mut pairs, word, t.range.clone());
                }
                _ => {}
            },
            "lean" => match word {
                "namespace" | "section" => stack.push(WordFrame { expected_close: ExpectedClose::Exact("end"), open_range: t.range.clone() }),
                "end" => {
                    pop_matching_word(&mut stack, &mut pairs, word, t.range.clone());
                }
                _ => {}
            },
            "sql" => {
                if word.eq_ignore_ascii_case("begin") || word.eq_ignore_ascii_case("case") {
                    stack.push(WordFrame { expected_close: ExpectedClose::CaseInsensitive("end"), open_range: t.range.clone() });
                } else if word.eq_ignore_ascii_case("end") {
                    pop_matching_word(&mut stack, &mut pairs, word, t.range.clone());
                }
            },
            "mermaid" => match word {
                "subgraph" => stack.push(WordFrame { expected_close: ExpectedClose::Exact("end"), open_range: t.range.clone() }),
                "end" => {
                    pop_matching_word(&mut stack, &mut pairs, word, t.range.clone());
                }
                _ => {}
            },
            _ => {}
        }
    }
    pairs
}

fn pop_matching_word(
    stack: &mut Vec<WordFrame>,
    pairs: &mut Vec<WordPair>,
    close_word: &str,
    close_range: Range<usize>,
) {
    if let Some(pos) = stack.iter().rposition(|frame| frame.expected_close.matches(close_word)) {
        let frame = stack.remove(pos);
        stack.truncate(pos);
        pairs.push(WordPair {
            open_range: frame.open_range,
            close_range,
        });
    }
}

pub fn match_latex_env(
    text: &str,
    tokens: &[CodeToken],
    offset: usize,
    include_delimiters: bool,
) -> Option<Range<usize>> {
    let pairs = collect_latex_pairs(text, tokens);
    let matched = pairs.into_iter().filter(|p| {
        let at_open = offset >= p.open_range.start && offset <= p.open_range.end;
        let at_close = offset >= p.close_range.start && offset <= p.close_range.end;
        at_open || at_close
    }).min_by_key(|p| p.close_range.end.saturating_sub(p.open_range.start))?;

    if include_delimiters {
        Some(matched.open_range.start..matched.close_range.end)
    } else {
        let inner_start = matched.open_range.end;
        let inner_end = matched.close_range.start;
        Some(trim_span(text, inner_start..inner_end))
    }
}

#[derive(Debug)]
struct LatexFrame {
    env_name: String,
    open_range: Range<usize>,
}

fn collect_latex_pairs(text: &str, tokens: &[CodeToken]) -> Vec<WordPair> {
    let mut pairs = Vec::new();
    let mut stack: Vec<LatexFrame> = Vec::new();
    let bytes = text.as_bytes();
    let n = bytes.len();
    let mut i = 0;

    while i < n {
        if let Some(t) = tokens.iter().find(|t| {
            matches!(t.kind, TokenKind::Comment | TokenKind::StringLit { .. })
                && t.range.contains(&i)
        }) {
            i = t.range.end;
            continue;
        }

        if bytes[i..].starts_with(b"\\begin{") {
            let start = i;
            let name_start = start + 7;
            if let Some(close_brace) = bytes[name_start..].iter().position(|&b| b == b'}') {
                if let Ok(name) = std::str::from_utf8(&bytes[name_start..name_start + close_brace]) {
                    let open_end = name_start + close_brace + 1;
                    stack.push(LatexFrame {
                        env_name: name.to_string(),
                        open_range: start..open_end,
                    });
                    i = open_end;
                    continue;
                }
            }
        } else if bytes[i..].starts_with(b"\\end{") {
            let start = i;
            let name_start = start + 5;
            if let Some(close_brace) = bytes[name_start..].iter().position(|&b| b == b'}') {
                if let Ok(name) = std::str::from_utf8(&bytes[name_start..name_start + close_brace]) {
                    let close_end = name_start + close_brace + 1;
                    if let Some(pos) = stack.iter().rposition(|f| f.env_name == name) {
                        let frame = stack.remove(pos);
                        stack.truncate(pos);
                        pairs.push(WordPair {
                            open_range: frame.open_range,
                            close_range: start..close_end,
                        });
                    }
                    i = close_end;
                    continue;
                }
            }
        }
        let ch_len = text[i..].chars().next().map_or(1, |c| c.len_utf8());
        i += ch_len;
    }

    pairs
}

pub fn trim_span(text: &str, span: Range<usize>) -> Range<usize> {
    if span.start >= span.end || span.end > text.len() {
        let pos = span.start.min(text.len());
        return pos..pos;
    }
    let sub = &text[span.start..span.end];
    let trimmed = sub.trim();
    if trimmed.is_empty() {
        return span.start..span.start;
    }
    let leading = sub.len() - sub.trim_start().len();
    let trailing = sub.len() - sub.trim_end().len();
    let start = (span.start + leading).min(span.end);
    let end = span.end.saturating_sub(trailing).max(start);
    start..end
}
