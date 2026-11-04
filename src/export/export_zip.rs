//! Minimal ZIP writer (STORED entries only) for the OpenXML container.
use super::ExportError;

fn crc32(data: &[u8]) -> u32 {
    let mut crc = !0u32;
    for &byte in data {
        crc ^= byte as u32;
        for _ in 0..8 {
            crc = (crc >> 1) ^ (0xEDB8_8320 & 0u32.wrapping_sub(crc & 1));
        }
    }
    !crc
}

fn u16le(out: &mut Vec<u8>, value: u16) {
    out.extend_from_slice(&value.to_le_bytes());
}

fn u32le(out: &mut Vec<u8>, value: u32) {
    out.extend_from_slice(&value.to_le_bytes());
}

fn too_large() -> ExportError {
    ExportError::ResourceLimit(crate::theme::tr("文件超过 4 GB", "File exceeds 4 GB"))
}

pub(super) fn zip_store(files: &[(&str, &[u8])]) -> Result<Vec<u8>, ExportError> {
    const DOS_DATE: u16 = 0x0021; // 1980-01-01
    const UTF8_NAMES: u16 = 0x0800;
    let mut out = Vec::new();
    let mut central = Vec::new();
    for (name, data) in files {
        let offset = u32::try_from(out.len()).map_err(|_| too_large())?;
        let size = u32::try_from(data.len()).map_err(|_| too_large())?;
        let crc = crc32(data);
        let name_len = u16::try_from(name.len()).map_err(|_| too_large())?;
        u32le(&mut out, 0x0403_4b50);
        u16le(&mut out, 20);
        u16le(&mut out, UTF8_NAMES);
        u16le(&mut out, 0);
        u16le(&mut out, 0);
        u16le(&mut out, DOS_DATE);
        u32le(&mut out, crc);
        u32le(&mut out, size);
        u32le(&mut out, size);
        u16le(&mut out, name_len);
        u16le(&mut out, 0);
        out.extend_from_slice(name.as_bytes());
        out.extend_from_slice(data);

        u32le(&mut central, 0x0201_4b50);
        u16le(&mut central, 20);
        u16le(&mut central, 20);
        u16le(&mut central, UTF8_NAMES);
        u16le(&mut central, 0);
        u16le(&mut central, 0);
        u16le(&mut central, DOS_DATE);
        u32le(&mut central, crc);
        u32le(&mut central, size);
        u32le(&mut central, size);
        u16le(&mut central, name_len);
        u16le(&mut central, 0);
        u16le(&mut central, 0);
        u16le(&mut central, 0);
        u16le(&mut central, 0);
        u32le(&mut central, 0);
        u32le(&mut central, offset);
        central.extend_from_slice(name.as_bytes());
    }
    let directory_offset = u32::try_from(out.len()).map_err(|_| too_large())?;
    let directory_size = u32::try_from(central.len()).map_err(|_| too_large())?;
    let count = u16::try_from(files.len()).map_err(|_| too_large())?;
    out.extend_from_slice(&central);
    u32le(&mut out, 0x0605_4b50);
    u16le(&mut out, 0);
    u16le(&mut out, 0);
    u16le(&mut out, count);
    u16le(&mut out, count);
    u32le(&mut out, directory_size);
    u32le(&mut out, directory_offset);
    u16le(&mut out, 0);
    Ok(out)
}
