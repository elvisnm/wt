#[cfg(test)]
mod tests {
    use crate::app::key_to_bytes;
    use crossterm::event::{KeyCode, KeyEvent, KeyModifiers};

    fn key(code: KeyCode) -> KeyEvent {
        KeyEvent::new(code, KeyModifiers::NONE)
    }

    fn ctrl(c: char) -> KeyEvent {
        KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL)
    }

    fn alt(c: char) -> KeyEvent {
        KeyEvent::new(KeyCode::Char(c), KeyModifiers::ALT)
    }

    #[test]
    fn regular_char() {
        assert_eq!(key_to_bytes(&key(KeyCode::Char('a'))), Some(vec![b'a']));
        assert_eq!(key_to_bytes(&key(KeyCode::Char('Z'))), Some(vec![b'Z']));
    }

    #[test]
    fn enter() {
        assert_eq!(key_to_bytes(&key(KeyCode::Enter)), Some(vec![b'\r']));
    }

    #[test]
    fn backspace() {
        assert_eq!(key_to_bytes(&key(KeyCode::Backspace)), Some(vec![0x7f]));
    }

    #[test]
    fn escape() {
        assert_eq!(key_to_bytes(&key(KeyCode::Esc)), Some(vec![0x1b]));
    }

    #[test]
    fn tab() {
        assert_eq!(key_to_bytes(&key(KeyCode::Tab)), Some(vec![b'\t']));
    }

    #[test]
    fn ctrl_c() {
        assert_eq!(key_to_bytes(&ctrl('c')), Some(vec![3])); // Ctrl+C = 0x03
    }

    #[test]
    fn ctrl_a() {
        assert_eq!(key_to_bytes(&ctrl('a')), Some(vec![1])); // Ctrl+A = 0x01
    }

    #[test]
    fn ctrl_z() {
        assert_eq!(key_to_bytes(&ctrl('z')), Some(vec![26])); // Ctrl+Z = 0x1a
    }

    #[test]
    fn alt_char() {
        let result = key_to_bytes(&alt('x'));
        assert!(result.is_some());
        let bytes = result.unwrap();
        assert_eq!(bytes[0], 0x1b); // ESC prefix
        assert_eq!(bytes[1], b'x');
    }

    #[test]
    fn arrow_keys() {
        assert_eq!(key_to_bytes(&key(KeyCode::Up)), Some(b"\x1b[A".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::Down)), Some(b"\x1b[B".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::Right)), Some(b"\x1b[C".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::Left)), Some(b"\x1b[D".to_vec()));
    }

    #[test]
    fn function_keys() {
        assert_eq!(key_to_bytes(&key(KeyCode::F(1))), Some(b"\x1bOP".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::F(5))), Some(b"\x1b[15~".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::F(12))), Some(b"\x1b[24~".to_vec()));
    }

    #[test]
    fn special_keys() {
        assert_eq!(key_to_bytes(&key(KeyCode::Home)), Some(b"\x1b[H".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::End)), Some(b"\x1b[F".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::Delete)), Some(b"\x1b[3~".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::PageUp)), Some(b"\x1b[5~".to_vec()));
        assert_eq!(key_to_bytes(&key(KeyCode::PageDown)), Some(b"\x1b[6~".to_vec()));
    }

    #[test]
    fn unicode_char() {
        let result = key_to_bytes(&key(KeyCode::Char('ñ')));
        assert!(result.is_some());
        assert_eq!(result.unwrap().len(), 2); // ñ is 2 bytes in UTF-8
    }
}
