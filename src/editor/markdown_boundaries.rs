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

/// Line count for non-prose HTML blocks (`<!-- ... -->`, `<!...>`, `<?...?>`, `<pre>`, `<script>`, `<style>`, `<svg>`, `<iframe>`).
pub(super) fn raw_html_lines(lines: &[&str]) -> Option<usize> {
    let first_raw = lines.first()?;
    let body = first_raw.trim_start_matches(' ');
    if first_raw.len() - body.len() > 3 || !body.starts_with('<') { return None; }
    if body.starts_with("<!--") {
        let end = lines.iter().position(|l| l.contains("-->")).map_or(lines.len(), |i| i + 1);
        return Some(end);
    }
    if body.starts_with("<?") {
        let end = lines.iter().position(|l| l.contains("?>")).map_or(lines.len(), |i| i + 1);
        return Some(end);
    }
    if body.starts_with("<!") {
        let end = lines.iter().position(|l| l.contains('>')).map_or(lines.len(), |i| i + 1);
        return Some(end);
    }
    let lower = body.to_ascii_lowercase();
    for tag in ["pre", "script", "style", "svg", "iframe"] {
        if let Some(rest) = lower.strip_prefix(&format!("<{tag}"))
            && (rest.is_empty() || rest.starts_with(['>', '/', ' ', '\t', '\r', '\n']))
        {
            let close = format!("</{tag}>");
            if rest.starts_with("/>") || rest.contains("/>") && !rest.contains('>') { return Some(1); }
            let end = lines.iter().position(|l| l.to_ascii_lowercase().contains(&close))
                .map_or_else(|| lines.iter().skip(1).position(|l| l.trim().is_empty()).map_or(lines.len(), |i| i + 1), |i| i + 1);
            return Some(end);
        }
    }
    None
}
