use std::sync::atomic::{AtomicU8, Ordering};

/// 0 = idle, 1 = playing, 2 = finished (waiting for auto-close)
static STATE: AtomicU8 = AtomicU8::new(0);

/// Embedded HeiHei audio (Moana chicken sound effect).
const HEIHEI_MP3: &[u8] = include_bytes!("../heihei.mp3");

/// Play the HeiHei sound effect (non-blocking, prevents concurrent playback).
pub fn play() {
    if STATE.load(Ordering::SeqCst) != 0 {
        return; // Already playing or waiting
    }
    STATE.store(1, Ordering::SeqCst);

    std::thread::spawn(|| {
        let tmp = std::env::temp_dir().join("wt-heihei.mp3");
        if std::fs::write(&tmp, HEIHEI_MP3).is_ok() {
            let _ = std::process::Command::new("afplay")
                .arg(&tmp)
                .status();
            let _ = std::fs::remove_file(&tmp);
        }
        // Wait 1s after audio ends, then signal finished
        std::thread::sleep(std::time::Duration::from_secs(1));
        STATE.store(2, Ordering::SeqCst);
    });
}

/// Check if playback finished and should auto-close. Resets state to idle.
pub fn should_close() -> bool {
    if STATE.load(Ordering::SeqCst) == 2 {
        STATE.store(0, Ordering::SeqCst);
        true
    } else {
        false
    }
}
