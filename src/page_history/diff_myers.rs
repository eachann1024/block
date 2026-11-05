//! Bounded Myers diff. Index checks fall back to a full replace instead of panicking.
use super::DiffOp;

pub(super) fn myers_diff(a: &[u64], b: &[u64]) -> Vec<DiffOp> {
    let n = a.len();
    let m = b.len();
    if n == 0 {
        return vec![DiffOp::Insert; m];
    }
    if m == 0 {
        return vec![DiffOp::Delete; n];
    }

    let max_d = (n + m).min(2048);
    // k is [-max_d, max_d]; the forward step also reads k±1.
    let offset = max_d as isize + 1;
    let slots = 2 * max_d + 3;
    let mut v = vec![0usize; slots];
    let mut trace = Vec::with_capacity(max_d + 1);
    let mut reached_d = None;

    for d in 0..=max_d {
        trace.push(v.clone());
        let d_i = d as isize;
        let mut k = -d_i;
        let mut found = false;
        while k <= d_i {
            let Some(k_idx) = slot(k, offset, slots) else { return replace_all(n, m) };
            let from_below = if k == -d_i {
                true
            } else if k == d_i {
                false
            } else {
                let Some(left) = slot(k - 1, offset, slots) else { return replace_all(n, m) };
                let Some(down) = slot(k + 1, offset, slots) else { return replace_all(n, m) };
                v[left] < v[down]
            };
            let mut x = if from_below {
                let Some(down) = slot(k + 1, offset, slots) else { return replace_all(n, m) };
                v[down]
            } else {
                let Some(left) = slot(k - 1, offset, slots) else { return replace_all(n, m) };
                v[left].saturating_add(1)
            };
            if x > n { return replace_all(n, m); }
            let y_i = x as isize - k;
            if y_i < 0 { return replace_all(n, m); }
            let mut y = y_i as usize;
            while x < n && y < m && a.get(x) == b.get(y) {
                x += 1;
                y += 1;
            }
            v[k_idx] = x;
            if x >= n && y >= m {
                reached_d = Some(d);
                found = true;
                break;
            }
            k += 2;
        }
        if found { break; }
    }

    let Some(d) = reached_d else { return replace_all(n, m) };
    backtrack(&trace, d, n, m, offset, slots).unwrap_or_else(|| replace_all(n, m))
}

fn slot(k: isize, offset: isize, slots: usize) -> Option<usize> {
    let index = k.checked_add(offset)?;
    if index < 0 { return None; }
    let index = index as usize;
    (index < slots).then_some(index)
}

fn replace_all(n: usize, m: usize) -> Vec<DiffOp> {
    let mut ops = Vec::with_capacity(n + m);
    ops.extend(std::iter::repeat(DiffOp::Delete).take(n));
    ops.extend(std::iter::repeat(DiffOp::Insert).take(m));
    ops
}

fn backtrack(
    trace: &[Vec<usize>],
    d: usize,
    n: usize,
    m: usize,
    offset: isize,
    slots: usize,
) -> Option<Vec<DiffOp>> {
    let mut ops = Vec::new();
    let mut x = n;
    let mut y = m;
    for step in (1..=d).rev() {
        let v_prev = trace.get(step)?;
        let k = x as isize - y as isize;
        let step_i = step as isize;
        let from_below = if k == -step_i {
            true
        } else if k == step_i {
            false
        } else {
            let left = v_prev.get(slot(k - 1, offset, slots)?)?;
            let down = v_prev.get(slot(k + 1, offset, slots)?)?;
            left < down
        };
        let prev_k = if from_below { k + 1 } else { k - 1 };
        let prev_x = *v_prev.get(slot(prev_k, offset, slots)?)?;
        let prev_y_i = prev_x as isize - prev_k;
        if prev_y_i < 0 { return None; }
        let prev_y = prev_y_i as usize;
        if prev_x > x || prev_y > y { return None; }
        while x > prev_x && y > prev_y {
            ops.push(DiffOp::Keep);
            x -= 1;
            y -= 1;
        }
        if x == prev_x && y == prev_y + 1 {
            ops.push(DiffOp::Insert);
            y -= 1;
        } else if y == prev_y && x == prev_x + 1 {
            ops.push(DiffOp::Delete);
            x -= 1;
        } else {
            return None;
        }
    }
    if x != y && (x == 0 || y == 0) {
        // The unmatched prefix is only deletes or only inserts.
    } else if x != y {
        return None;
    }
    while x > 0 && y > 0 {
        ops.push(DiffOp::Keep);
        x -= 1;
        y -= 1;
    }
    while x > 0 {
        ops.push(DiffOp::Delete);
        x -= 1;
    }
    while y > 0 {
        ops.push(DiffOp::Insert);
        y -= 1;
    }
    ops.reverse();
    Some(ops)
}
