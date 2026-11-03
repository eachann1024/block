//! Deterministic natural ordering, including arbitrarily long digit runs.
use std::cmp::Ordering;

pub fn natural_cmp(mut a: &str, mut b: &str) -> Ordering {
    loop {
        match (a.chars().next(), b.chars().next()) {
            (None, None) => return Ordering::Equal,
            (None, Some(_)) => return Ordering::Less,
            (Some(_), None) => return Ordering::Greater,
            (Some(ca), Some(cb)) if ca.is_ascii_digit() && cb.is_ascii_digit() => {
                let na = take_number(&mut a);
                let nb = take_number(&mut b);
                let sa = na.trim_start_matches('0');
                let sb = nb.trim_start_matches('0');
                let ord = sa.len().cmp(&sb.len()).then_with(|| sa.cmp(sb))
                    .then_with(|| na.len().cmp(&nb.len()));
                if ord != Ordering::Equal { return ord; }
            }
            (Some(ca), Some(cb)) => {
                a = &a[ca.len_utf8()..];
                b = &b[cb.len_utf8()..];
                let ord = ca.to_lowercase().cmp(cb.to_lowercase());
                if ord != Ordering::Equal { return ord; }
            }
        }
    }
}

fn take_number<'a>(text: &mut &'a str) -> &'a str {
    let len = text.bytes().take_while(u8::is_ascii_digit).count();
    let (number, rest) = text.split_at(len);
    *text = rest;
    number
}
