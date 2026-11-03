//! Case-insensitive natural ordering: digit runs compare numerically, the rest
//! by lowercased char.

pub fn natural_cmp(a: &str, b: &str) -> std::cmp::Ordering {
    let (mut a, mut b) = (a.chars().peekable(), b.chars().peekable());
    loop {
        match (a.peek().copied(), b.peek().copied()) {
            (None, None) => return std::cmp::Ordering::Equal,
            (None, Some(_)) => return std::cmp::Ordering::Less,
            (Some(_), None) => return std::cmp::Ordering::Greater,
            (Some(ca), Some(cb)) if ca.is_ascii_digit() && cb.is_ascii_digit() => {
                let (na, la) = take_number(&mut a);
                let (nb, lb) = take_number(&mut b);
                match (na.cmp(&nb), la.cmp(&lb)) {
                    (std::cmp::Ordering::Equal, len) if len != std::cmp::Ordering::Equal => {
                        return len;
                    }
                    (ord, _) if ord != std::cmp::Ordering::Equal => return ord,
                    _ => {}
                }
            }
            (Some(ca), Some(cb)) => {
                a.next();
                b.next();
                let ord = ca.to_lowercase().cmp(cb.to_lowercase());
                if ord != std::cmp::Ordering::Equal {
                    return ord;
                }
            }
        }
    }
}

/// (numeric value, digit count) so "01" sorts before "1" when values tie.
fn take_number(chars: &mut std::iter::Peekable<std::str::Chars<'_>>) -> (u128, usize) {
    let mut value = 0u128;
    let mut len = 0usize;
    let mut overflow = false;
    while let Some(c) = chars.peek().copied().filter(|c| c.is_ascii_digit()) {
        chars.next();
        len += 1;
        if let Some(next) = value.checked_mul(10).and_then(|v| v.checked_add((c as u8 - b'0') as u128)) {
            value = next;
        } else {
            overflow = true;
        }
    }
    (if overflow { u128::MAX } else { value }, len)
}
