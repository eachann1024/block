use super::{blocks, super::model::Kind};

pub(super) fn table_lines(lines: &[&str]) -> usize {
    let mut depth = 0usize;
    for (index, line) in lines.iter().enumerate() {
        let lower = line.to_ascii_lowercase();
        let mut rest = lower.as_str();
        while let Some(at) = rest.find('<') {
            rest = &rest[at..];
            if rest.starts_with("</table>") {
                depth = depth.saturating_sub(1);
                rest = &rest[8..];
                if depth == 0 { return index + 1; }
            } else if rest.strip_prefix("<table").is_some_and(|s| s.starts_with(['>', '/', ' ', '\t'])) {
                depth += 1;
                rest = &rest[6..];
            } else { rest = &rest[1..]; }
        }
    }
    let close = lines.iter().position(|line| line.to_ascii_lowercase().contains("</table>"));
    let next_table = lines.iter().enumerate().skip(1).find(|(_, line)| {
        let line = line.trim_start().to_ascii_lowercase();
        line.strip_prefix("<table").is_some_and(|rest| rest.starts_with(['>', '/', ' ', '\t']))
    }).map(|(index, _)| index);
    if let Some(end) = close && next_table.is_none_or(|next| end < next) {
        return end + 1;
    }
    let limit = next_table.unwrap_or(lines.len());
    lines[..limit].iter().enumerate().skip(1).find(|(_, line)| {
        let trimmed = line.trim();
        let hashes = trimmed.bytes().take_while(|byte| *byte == b'#').count();
        trimmed.is_empty() || (1..=6).contains(&hashes)
            && trimmed[hashes..].starts_with([' ', '\t'])
    }).map_or(limit, |(index, _)| index)
}

pub(super) fn unsupported(lines: &[&str]) -> Option<usize> {
    let first = lines.first()?.trim_start();
    let (kind, _, _, indent, _, _) = blocks::classify(lines[0].trim_end_matches(['\r', '\n']));
    if matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo) {
        let count = 1 + lines.iter().skip(1).take_while(|line| {
            if line.trim().is_empty() { return false; }
            let (kind, _, _, child_indent, _, _) = blocks::classify(line.trim_end_matches(['\r', '\n']));
            child_indent >= indent && (child_indent > indent || matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo))
        }).count();
        let continuation = lines[1..count].iter().any(|line| {
            let (kind, _, _, child_indent, _, _) = blocks::classify(line.trim_end_matches(['\r', '\n']));
            child_indent > indent && !matches!(kind, Kind::Bullet | Kind::Numbered | Kind::Todo)
        });
        if continuation { return Some(count); }
    }
    if first.starts_with(":::") && first.trim() != ":::" {
        let end = lines.iter().enumerate().skip(1).find(|(_, line)| line.trim() == ":::")
            .map(|(index, _)| index + 1)
            .unwrap_or_else(|| lines.iter().skip(1).position(|line| line.trim().is_empty())
                .map_or(lines.len(), |index| index + 1));
        return Some(end);
    }
    if first.starts_with("[^" ) && first.contains("]:") {
        let count = 1 + lines.iter().skip(1).take_while(|line|
            line.starts_with("    ") || line.starts_with('\t')).count();
        return Some(count);
    }
    if first.starts_with('>') {
        let count = lines.iter().take_while(|line| line.trim_start().starts_with('>')).count();
        if lines[..count].iter().any(|line| blocks::classify(line.trim_end_matches(['\r', '\n'])).5) {
            return Some(count);
        }
    }
    None
}
