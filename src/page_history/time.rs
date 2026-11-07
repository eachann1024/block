//! Relative and localized timestamp formatting for Page History versions.
use std::time::{SystemTime, UNIX_EPOCH};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct DateTimeParts {
    pub year: i32,
    pub month: u32, // 1..=12
    pub day: u32,   // 1..=31
    pub hour: u32,  // 0..=23
    pub min: u32,   // 0..=59
    pub sec: u32,   // 0..=59
}

#[cfg(all(unix, target_pointer_width = "64"))]
fn local_parts(secs: i64) -> DateTimeParts {
    #[repr(C)]
    struct Tm {
        tm_sec: i32,
        tm_min: i32,
        tm_hour: i32,
        tm_mday: i32,
        tm_mon: i32,
        tm_year: i32,
        tm_wday: i32,
        tm_yday: i32,
        tm_isdst: i32,
        tm_gmtoff: i64,
        tm_zone: *const std::ffi::c_char,
    }
    unsafe extern "C" {
        fn localtime_r(timep: *const i64, result: *mut Tm) -> *mut Tm;
    }
    unsafe {
        let mut out = std::mem::zeroed::<Tm>();
        if !localtime_r(&secs, &mut out).is_null() {
            return DateTimeParts {
                year: out.tm_year + 1900,
                month: (out.tm_mon + 1) as u32,
                day: out.tm_mday as u32,
                hour: out.tm_hour as u32,
                min: out.tm_min as u32,
                sec: (out.tm_sec as u32).min(59),
            };
        }
    }
    utc_parts(secs)
}

/// 32-bit `time_t` is not `i64`. Falling back avoids a mismatched `localtime_r` ABI.
#[cfg(all(unix, not(target_pointer_width = "64")))]
fn local_parts(secs: i64) -> DateTimeParts {
    utc_parts(secs)
}

#[cfg(windows)]
fn local_parts(secs: i64) -> DateTimeParts {
    #[repr(C)]
    struct Tm {
        tm_sec: i32,
        tm_min: i32,
        tm_hour: i32,
        tm_mday: i32,
        tm_mon: i32,
        tm_year: i32,
        tm_wday: i32,
        tm_yday: i32,
        tm_isdst: i32,
    }
    unsafe extern "C" {
        fn _localtime64_s(result: *mut Tm, timep: *const i64) -> i32;
    }
    unsafe {
        let mut out = std::mem::zeroed::<Tm>();
        if _localtime64_s(&mut out, &secs) == 0 {
            return DateTimeParts {
                year: out.tm_year + 1900,
                month: (out.tm_mon + 1) as u32,
                day: out.tm_mday as u32,
                hour: out.tm_hour as u32,
                min: out.tm_min as u32,
                sec: (out.tm_sec as u32).min(59),
            };
        }
    }
    utc_parts(secs)
}

#[cfg(not(any(unix, windows)))]
fn local_parts(secs: i64) -> DateTimeParts {
    utc_parts(secs)
}

fn utc_parts(secs: i64) -> DateTimeParts {
    let days = (secs.div_euclid(86_400)) as i64;
    let day_secs = secs.rem_euclid(86_400) as u32;
    let hour = day_secs / 3600;
    let min = (day_secs % 3600) / 60;
    let sec = day_secs % 60;

    let z = days + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = (if m <= 2 { y + 1 } else { y }) as i32;
    DateTimeParts { year, month: m, day: d, hour, min, sec }
}

fn current_timestamp() -> u64 {
    #[cfg(not(target_family = "wasm"))]
    {
        SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0)
    }
    #[cfg(target_family = "wasm")]
    {
        (js_sys::Date::now() / 1000.) as u64
    }
}

pub fn now_seconds() -> u64 {
    current_timestamp()
}

pub fn now_nanos() -> u128 {
    #[cfg(not(target_family = "wasm"))]
    {
        SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0)
    }
    #[cfg(target_family = "wasm")]
    {
        (js_sys::Date::now() * 1_000_000.) as u128
    }
}

pub fn format_datetime_ymdhms(timestamp: u64) -> String {
    let target = local_parts(timestamp as i64);
    if crate::theme::is_en() {
        format!(
            "{:04}-{:02}-{:02} {:02}:{:02}:{:02}",
            target.year, target.month, target.day, target.hour, target.min, target.sec
        )
    } else {
        format!(
            "{:04}年{:02}月{:02}日 {:02}:{:02}:{:02}",
            target.year, target.month, target.day, target.hour, target.min, target.sec
        )
    }
}

pub fn format_version_time(timestamp: u64) -> String {
    let now = current_timestamp();
    let now_parts = local_parts(now as i64);
    let target = local_parts(timestamp as i64);

    let is_same_day = now_parts.year == target.year && now_parts.month == target.month && now_parts.day == target.day;
    let is_yesterday = is_previous_day(now_parts, target);

    let is_en = crate::theme::is_en();
    if is_same_day {
        if is_en {
            format!("Today {:02}:{:02}", target.hour, target.min)
        } else {
            format!("今天 {:02}:{:02}", target.hour, target.min)
        }
    } else if is_yesterday {
        if is_en {
            format!("Yesterday {:02}:{:02}", target.hour, target.min)
        } else {
            format!("昨天 {:02}:{:02}", target.hour, target.min)
        }
    } else if now_parts.year == target.year {
        if is_en {
            let m = en_month(target.month);
            format!("{} {}, {:02}:{:02}", m, target.day, target.hour, target.min)
        } else {
            format!("{}月{}日 {:02}:{:02}", target.month, target.day, target.hour, target.min)
        }
    } else if is_en {
        let m = en_month(target.month);
        format!("{} {}, {} {:02}:{:02}", m, target.day, target.year, target.hour, target.min)
    } else {
        format!("{}年{}月{}日 {:02}:{:02}", target.year, target.month, target.day, target.hour, target.min)
    }
}

fn is_previous_day(now: DateTimeParts, target: DateTimeParts) -> bool {
    let now_days = days_from_civil(now.year, now.month, now.day);
    let target_days = days_from_civil(target.year, target.month, target.day);
    now_days == target_days + 1
}

fn days_from_civil(y: i32, m: u32, d: u32) -> i64 {
    let y = y as i64 - if m <= 2 { 1 } else { 0 };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = (y - era * 400) as u32;
    let doy = (153 * (if m > 2 { m - 3 } else { m + 9 }) + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe as i64 - 719_468
}

fn en_month(m: u32) -> &'static str {
    match m {
        1 => "Jan", 2 => "Feb", 3 => "Mar", 4 => "Apr", 5 => "May", 6 => "Jun",
        7 => "Jul", 8 => "Aug", 9 => "Sep", 10 => "Oct", 11 => "Nov", 12 => "Dec",
        _ => "Jan",
    }
}
