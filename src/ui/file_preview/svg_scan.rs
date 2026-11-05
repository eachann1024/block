//! Structural SVG gate. A real scan, not a regex: only a static document with
//! no DTD, custom entities, active tags, or external resources is returned.

pub const MAX_SVG_BYTES: usize = 2 * 1024 * 1024;
pub const MAX_SVG_ELEMENTS: usize = 5_000;
const MAX_DEPTH: usize = 64;
const MAX_ATTR: usize = 256 * 1024;
pub const MAX_SVG_EDGE: f32 = 4096.0;
pub const MAX_SVG_PIXELS: f32 = 4_000_000.0;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct SvgSize {
    pub width: f32,
    pub height: f32,
}

pub fn sanitize_svg(source: &str) -> Result<SvgSize, String> {
    if source.len() > MAX_SVG_BYTES || source.as_bytes().contains(&0) {
        return Err(super::copy("SVG 超过大小或编码限制", "SVG exceeds the size or encoding limit"));
    }
    let mut scan = Scan { s: source, i: 0, elements: 0 };
    scan.skip_bom();
    let mut stack: Vec<String> = Vec::new();
    let mut size = None;
    while !scan.eof() {
        if scan.starts("<") {
            scan.markup(&mut stack, &mut size)?;
        } else {
            let text = scan.text()?;
            if stack.iter().any(|name| name.eq_ignore_ascii_case("style")) {
                super::svg_attr::check_resource(&text)?;
            } else if stack.is_empty() && text.chars().any(|ch| !ch.is_whitespace()) {
                return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
            }
        }
    }
    if !stack.is_empty() {
        return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
    }
    let size = size.ok_or_else(|| super::copy("SVG 缺少可计算的宽高", "SVG has no usable width and height"))?;
    if !(size.width.is_finite() && size.height.is_finite())
        || size.width <= 0.0
        || size.height <= 0.0
        || size.width > MAX_SVG_EDGE
        || size.height > MAX_SVG_EDGE
        || size.width * size.height > MAX_SVG_PIXELS
    {
        return Err(super::copy("SVG 尺寸超出预览限制", "SVG size exceeds the preview limit"));
    }
    Ok(size)
}

struct Scan<'a> {
    s: &'a str,
    i: usize,
    elements: usize,
}

impl<'a> Scan<'a> {
    fn eof(&self) -> bool { self.i >= self.s.len() }
    fn rest(&self) -> &'a str { &self.s[self.i..] }
    fn starts(&self, prefix: &str) -> bool { self.rest().starts_with(prefix) }
    fn skip_bom(&mut self) { if self.starts("\u{feff}") { self.i += "\u{feff}".len(); } }

    fn bump(&mut self) -> Result<char, String> {
        let ch = self.rest().chars().next().ok_or_else(|| super::copy("SVG 结构无效", "Invalid SVG structure"))?;
        self.i += ch.len_utf8();
        Ok(ch)
    }

    fn markup(&mut self, stack: &mut Vec<String>, size: &mut Option<SvgSize>) -> Result<(), String> {
        self.bump()?;
        if self.starts("!--") {
            self.i += 3;
            let end = self.rest().find("-->").ok_or_else(|| super::copy("SVG 结构无效", "Invalid SVG structure"))?;
            self.i += end + 3;
            return Ok(());
        }
        if self.starts("![CDATA[") {
            self.i += 8;
            let end = self.rest().find("]]>").ok_or_else(|| super::copy("SVG 结构无效", "Invalid SVG structure"))?;
            let text = self.rest()[..end].to_string();
            self.i += end + 3;
            if stack.iter().any(|name| name.eq_ignore_ascii_case("style")) { super::svg_attr::check_resource(&text)?; }
            return Ok(());
        }
        if self.starts("!") {
            return Err(super::copy("不支持 DTD、实体或活动声明", "DTD, entities, or active declarations are not supported"));
        }
        if self.starts("?") {
            self.bump()?;
            let target = self.read_name()?.to_ascii_lowercase();
            let end = self.rest().find("?>").ok_or_else(|| super::copy("SVG 结构无效", "Invalid SVG structure"))?;
            self.i += end + 2;
            if target != "xml" {
                return Err(super::copy("不支持外部处理指令", "External processing instructions are not supported"));
            }
            return Ok(());
        }
        if self.starts("/") {
            self.bump()?;
            let name = self.read_name()?;
            self.skip_space();
            if self.bump()? != '>' {
                return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
            }
            if stack.pop().as_deref() != Some(name.as_str()) {
                return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
            }
            return Ok(());
        }
        let name = self.read_name()?;
        let local = super::svg_attr::local_name(&name).to_ascii_lowercase();
        if super::svg_attr::forbidden(&local) {
            return Err(super::copy("SVG 含有不支持的活动或外部元素", "SVG contains an unsupported active or external element"));
        }
        if stack.len() >= MAX_DEPTH {
            return Err(super::copy("SVG 过于复杂", "SVG is too complex"));
        }
        if stack.is_empty() && size.is_some() {
            return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
        }
        let mut attrs = Vec::new();
        loop {
            self.skip_space();
            if self.starts("/>") {
                self.i += 2;
                self.note_element(stack, size, &name, &local, &attrs, true)?;
                return Ok(());
            }
            if self.starts(">") {
                self.bump()?;
                self.note_element(stack, size, &name, &local, &attrs, false)?;
                return Ok(());
            }
            let attr = self.read_name()?;
            self.skip_space();
            if self.bump()? != '=' {
                return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
            }
            self.skip_space();
            let value = self.quoted()?;
            super::svg_attr::check_attr(&attr, &value)?;
            attrs.push((attr, value));
        }
    }

    fn note_element(
        &mut self,
        stack: &mut Vec<String>,
        size: &mut Option<SvgSize>,
        name: &str,
        local: &str,
        attrs: &[(String, String)],
        closed: bool,
    ) -> Result<(), String> {
        self.elements += 1;
        if self.elements > MAX_SVG_ELEMENTS {
            return Err(super::copy("SVG 过于复杂", "SVG is too complex"));
        }
        if stack.is_empty() {
            if local != "svg" {
                return Err(super::copy("根节点必须是 svg", "The root element must be svg"));
            }
            *size = Some(super::svg_attr::root_size(attrs)?);
        }
        if !closed { stack.push(name.to_string()); }
        Ok(())
    }

    fn text(&mut self) -> Result<String, String> {
        let mut out = String::new();
        while !self.eof() && !self.starts("<") {
            let ch = self.bump()?;
            if ch == '&' {
                out.push_str(&self.entity()?);
            } else {
                out.push(ch);
            }
            if out.len() > MAX_SVG_BYTES {
                return Err(super::copy("SVG 过于复杂", "SVG is too complex"));
            }
        }
        Ok(out)
    }

    fn quoted(&mut self) -> Result<String, String> {
        let quote = self.bump()?;
        if quote != '"' && quote != '\'' {
            return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
        }
        let mut out = String::new();
        loop {
            let ch = self.bump()?;
            if ch == quote { return Ok(out); }
            if ch == '<' { return Err(super::copy("SVG 结构无效", "Invalid SVG structure")); }
            if ch == '&' { out.push_str(&self.entity()?); } else { out.push(ch); }
            if out.len() > MAX_ATTR {
                return Err(super::copy("SVG 过于复杂", "SVG is too complex"));
            }
        }
    }

    fn entity(&mut self) -> Result<String, String> {
        if self.starts("#") {
            self.bump()?;
            let hex = self.starts("x") || self.starts("X");
            if hex { self.bump()?; }
            let start = self.i;
            let mut digits = 0;
            while self.rest().chars().next().is_some_and(|ch| if hex { ch.is_ascii_hexdigit() } else { ch.is_ascii_digit() }) {
                self.bump()?;
                digits += 1;
                if digits > 6 { return Err(super::copy("不支持自定义实体", "Custom entities are not supported")); }
            }
            if self.bump()? != ';' || digits == 0 {
                return Err(super::copy("不支持自定义实体", "Custom entities are not supported"));
            }
            let code = u32::from_str_radix(&self.s[start..self.i - 1], if hex { 16 } else { 10 })
                .map_err(|_| super::copy("不支持自定义实体", "Custom entities are not supported"))?;
            if code == 0 || code == 0x7F || (code < 32 && code != 9 && code != 10 && code != 13) {
                return Err(super::copy("不支持自定义实体", "Custom entities are not supported"));
            }
            return Ok(char::from_u32(code).ok_or_else(|| super::copy("不支持自定义实体", "Custom entities are not supported"))?.to_string());
        }
        let name = self.read_name()?;
        if self.bump()? != ';' {
            return Err(super::copy("不支持自定义实体", "Custom entities are not supported"));
        }
        match name.as_str() {
            "amp" => Ok("&".into()),
            "lt" => Ok("<".into()),
            "gt" => Ok(">".into()),
            "quot" => Ok("\"".into()),
            "apos" => Ok("'".into()),
            _ => Err(super::copy("不支持自定义实体", "Custom entities are not supported")),
        }
    }

    fn read_name(&mut self) -> Result<String, String> {
        let mut name = String::new();
        loop {
            let Some(ch) = self.rest().chars().next() else { break };
            if name.is_empty() && !ch.is_ascii_alphabetic() && ch != '_' && ch != ':' {
                break;
            }
            if !name.is_empty() && !ch.is_ascii_alphanumeric() && !matches!(ch, '_' | ':' | '-' | '.') {
                break;
            }
            if name.is_empty() && !ch.is_ascii_alphabetic() && ch != '_' {
                return Err(super::copy("SVG 结构无效", "Invalid SVG structure"));
            }
            name.push(ch);
            self.i += ch.len_utf8();
            if name.len() > 128 { return Err(super::copy("SVG 结构无效", "Invalid SVG structure")); }
        }
        if name.is_empty() { Err(super::copy("SVG 结构无效", "Invalid SVG structure")) } else { Ok(name) }
    }

    fn skip_space(&mut self) {
        while self.rest().chars().next().is_some_and(|ch| ch.is_whitespace()) {
            let ch = self.rest().chars().next().unwrap();
            self.i += ch.len_utf8();
        }
    }
}
