use std::sync::{Arc, Mutex};

use alacritty_terminal::grid::Dimensions;
use alacritty_terminal::term::cell::Flags as CellFlags;
use alacritty_terminal::Term;
use ratatui::prelude::*;
use ratatui::widgets::Widget;

use crate::pty::PtyEventListener;

/// Widget that renders an alacritty_terminal grid into a ratatui frame.
pub struct TermView<'a> {
    term: &'a Arc<Mutex<Term<PtyEventListener>>>,
}

impl<'a> TermView<'a> {
    pub fn new(term: &'a Arc<Mutex<Term<PtyEventListener>>>) -> Self {
        Self { term }
    }
}

impl<'a> Widget for TermView<'a> {
    fn render(self, area: Rect, buf: &mut Buffer) {
        let term = match self.term.lock() {
            Ok(t) => t,
            Err(_) => return,
        };

        let content = term.renderable_content();
        let display_offset = content.display_offset;
        let selection = content.selection;
        let grid = term.grid();
        let cols = grid.columns();
        let lines = grid.screen_lines();

        // Render grid content with display_offset for scrollback support
        for row in 0..lines.min(area.height as usize) {
            // Convert viewport row to grid line accounting for display_offset
            let grid_line = alacritty_terminal::index::Line(row as i32 - display_offset as i32);

            for col in 0..cols.min(area.width as usize) {
                let cell = &grid[grid_line][alacritty_terminal::index::Column(col)];
                let point = alacritty_terminal::index::Point {
                    line: grid_line,
                    column: alacritty_terminal::index::Column(col),
                };
                let is_selected = selection.as_ref().map_or(false, |s| s.contains(point));

                let x = area.x + col as u16;
                let y = area.y + row as u16;

                if x >= area.x + area.width || y >= area.y + area.height {
                    continue;
                }

                let c = if cell.c == '\0' || cell.c == ' ' { ' ' } else { cell.c };
                let fg = convert_color(cell.fg);
                let bg = convert_color(cell.bg);

                let mut style = Style::default().fg(fg).bg(bg);

                if cell.flags.contains(CellFlags::BOLD) {
                    style = style.bold();
                }
                if cell.flags.contains(CellFlags::ITALIC) {
                    style = style.italic();
                }
                if cell.flags.contains(CellFlags::UNDERLINE) || cell.flags.contains(CellFlags::DOUBLE_UNDERLINE) {
                    style = style.underlined();
                }
                if cell.flags.contains(CellFlags::INVERSE) {
                    style = style.fg(bg).bg(fg);
                }
                if cell.flags.contains(CellFlags::DIM) {
                    style = style.dim();
                }

                // Selection highlight — invert colors
                if is_selected {
                    style = Style::default().fg(Color::Black).bg(Color::White);
                }

                buf.set_string(x, y, c.to_string(), style);
            }
        }

        // Render cursor (only when not scrolled back)
        if display_offset == 0 {
            let cursor = content.cursor;
            let cx = area.x + cursor.point.column.0 as u16;
            let cy = area.y + cursor.point.line.0 as u16;
            if cx < area.x + area.width && cy < area.y + area.height {
                let existing = buf.cell(ratatui::layout::Position::new(cx, cy));
                if let Some(existing) = existing {
                    let fg = existing.bg;
                    let bg = existing.fg;
                    let symbol = existing.symbol().to_string();
                    buf.set_string(cx, cy, &symbol, Style::default().fg(fg).bg(bg));
                }
            }
        }

        // Scrollbar — only visible when scrolled back
        if display_offset > 0 {
            let total_h = grid.total_lines();
            let view_h = area.height as usize;
            if total_h > view_h {
                let track_h = view_h;
                let thumb_h = (view_h * view_h / total_h).max(1);
                let max_offset = total_h - view_h;
                let thumb_pos = if max_offset > 0 {
                    ((max_offset - display_offset.min(max_offset)) * (track_h - thumb_h)) / max_offset
                } else {
                    0
                };

                let scroll_x = area.x + area.width - 1;
                let thumb_style = Style::default().fg(Color::Indexed(240));
                let track_style = Style::default().fg(Color::Indexed(236));

                for i in 0..track_h {
                    let y = area.y + i as u16;
                    if y < area.y + area.height {
                        if i >= thumb_pos && i < thumb_pos + thumb_h {
                            buf.set_string(scroll_x, y, "█", thumb_style);
                        } else {
                            buf.set_string(scroll_x, y, "│", track_style);
                        }
                    }
                }
            }
        }
    }
}

fn convert_color(color: alacritty_terminal::vte::ansi::Color) -> Color {
    use alacritty_terminal::vte::ansi::{Color as AColor, NamedColor};

    match color {
        AColor::Named(named) => match named {
            NamedColor::Black => Color::Black,
            NamedColor::Red => Color::Red,
            NamedColor::Green => Color::Green,
            NamedColor::Yellow => Color::Yellow,
            NamedColor::Blue => Color::Blue,
            NamedColor::Magenta => Color::Magenta,
            NamedColor::Cyan => Color::Cyan,
            NamedColor::White => Color::White,
            NamedColor::BrightBlack => Color::DarkGray,
            NamedColor::BrightRed => Color::LightRed,
            NamedColor::BrightGreen => Color::LightGreen,
            NamedColor::BrightYellow => Color::LightYellow,
            NamedColor::BrightBlue => Color::LightBlue,
            NamedColor::BrightMagenta => Color::LightMagenta,
            NamedColor::BrightCyan => Color::LightCyan,
            NamedColor::BrightWhite => Color::White,
            NamedColor::Foreground => Color::Reset,
            NamedColor::Background => Color::Reset,
            _ => Color::Reset,
        },
        AColor::Spec(rgb) => Color::Rgb(rgb.r, rgb.g, rgb.b),
        AColor::Indexed(idx) => Color::Indexed(idx),
    }
}
