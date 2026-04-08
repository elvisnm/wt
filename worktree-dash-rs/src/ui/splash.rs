use ratatui::{
    prelude::*,
    widgets::Paragraph,
};

use super::style::*;

const HEIHEI_ART: &[&str] = &[
    r"                                                                  .......",
    r"                                                                  .+#-......",
    r"                                                                 ..##.#+#+#+ ...",
    r"                                                                 .###-###+#+..-.",
    r"                                                                 .#####+++###+#....",
    r"                                                                 .#++++########-#.",
    r"                                                                 .##+++++++###+#+",
    r"                                                                  .##+++++++++##......",
    r"                                                                  .###+++++++##++#+..",
    r"                                                                   .##+++++++#+-++-",
    r"                                                                ...###+#++++##+-+#+...",
    r"                                                               .########+#++##-+#####+",
    r"                                                              .##+..#####++###-+##+###.",
    r"                                                             ..........##+++#++#+#+....",
    r"                                                            ............##+#++++#+---...",
    r"                                                           ..............#####++-........",
    r"                                                           .##...........#+++++#++-....##.",
    r"                                                          ..##..........########+-+-...##-",
    r"                                                           ............###+-..##++--+-....",
    r"                                                           ...........+##......+#+------..",
    r"                                                            .-.-++----###.+#--+-#.....-.",
    r"                                                            .##-...+++##+.++++++#+-.....",
    r"                                                            .-######--###########+#####",
    r"                                                             ..-#++######+####+##-##+-",
    r"                                                               ..+++-+###-----+##.+..",
    r"                                                                .-##-#+##-+++-####",
    r"                                                                ...+-#+##++++-#+#+",
    r"                                                                  .-+####-+++-#+##",
    r"                                                                  .+.##+-++++-###+",
    r"                                                                  .-+..-++++-.+#+.",
    r"                                                                   -++-++++++-+--",
    r"                                                                   .+-.++++---+--",
    r"                                                                   .-+.-#++-++-+..",
    r"                                                                  ..#--.+++++++++..",
    r"                               +                                 .-#+-#.++#++-+--+..",
    r"                    -.........--+++-.....                         ---.+.--++-+-.-+-#.",
    r"                 ..-+++++##+#+###++++++++++-...                   -++++++--..+--#+-+.",
    r"               ..-###########################---.                .#+#+.#.-#+-+---+--.",
    r"            ---####+..++---++-.--.+---+-++++###++-             ...+-#..#.-+-----.++++...",
    r"            -###----+##++++++++###++++++++##++###+.             .+#+-+#.-+--##-#++#.#+",
    r"           +##-+-...-..----..-............--++++##-.           ..+++.#+--+--------+++--.",
    r"          .#+-+-.                          .--+++##..         ..#+#++#.#-+--+-+-+++-++.",
    r"         .#++-.                               .++-+#+-.     .-+#+##-#-.#-#.-+-+.#.#++++.",
    r"        .#+.-                                  -++++##-    ..+#-#+.-#+++-#-.+---+#-+#+#+.",
    r"        +#..                                     .--+##-  .+##++#++##.++.#-.#+.+.#++#++#..",
    r"       -#+-                                       +#++#- .-+###+#+++##.#--#-.##.#.##++++++#.",
    r"       .#-                   ..........-     ....--######...-++++-##+-#++#.+#-.#+-#-#+####-##-",
    r"       ++-               ..-+#+######+++++-+###-.....+###...+###-##++#+++##.##--#+-#+++++++.#+.",
    r"       ++             .-+###+++#+-++#++++###++####-.--++-..#####+#+..#-#--#+-#+.+#--##-#+++++##+",
    r"      .+-           .-+#++++-.##    -+####+++++--+##+##..-#########+####+#++.+#+#++-#-+#+#+++.###-+.",
    r"      ...          .++##++.          ++##+++########+...#+###+#######+##+...#+++-#++-#-+#++++--###.#.",
    r"                  .+####.           -+########...+++#######+##++##############.#.+++.#.++++#+.###++#-",
    r"                 .++#++          ..-+#####-..####+-++++#+++.-++##########+#######.+-.#..#.++.###+-#+",
    r"                .-###. ..........-+##+##...##+#+-+#+-+#+++#++++-++########+#++##+###-#.+#+.+###++#+.",
    r"                -+#+-     .-+####+--+##.####-#-+++#+####+######+-++-####+++##+##+++++############+.",
    r"               .++#-        .....-+###.##-#+#-+###+####+##++######+#+-+--+-#####+-++##+###+##+...",
    r"               -#-                 ##.#+..#+#+##..###+##+##+###########++############++##+....",
    r"              .+-.                -##.#...#+##..+##+###+###+#+--++#########++++##+##+##-.",
    r"             .+-.                .+##-+...#+#.+#######+#+##++#-.-..++###+++++##+#####-.",
    r"             +-.                .-##-+-...###+######+###-###++###+-#++#####+#+####+..",
    r"            -+.                .-###+#...+####.-######+########++--+-++##+#######-",
    r"           ...                .+####.#...#####-.###+.#+######+#+#####+#+########+.",
    r"           .                 +#+-- .+..+#####+..#######-+#---+#+--++-++########+.",
    r"                           ..--+  .-..  ####. ####+ +####+   +####+##+  .#####+.",
    r"                                 -...-  ###-  -#++  #####+   -++-++++.  .#####.",
    r"                                ....   -+##+  +###  +##+--   -###+-++   .####+.",
    r"                               ...     ###.         -#...    -###+++     -###-",
    r"                                       ...          .##       #####.     .+++.",
    r"                                                     ..       .--..      .++-..",
    r"                                                              .++#+      .###.",
    r"                                                              .-...      .+++.",
    r"                                                              .###.      .+++.",
    r"                                                              .-.-       .+++.",
    r"                                                              .--+       .+++.",
    r"                                                              .-+#.      .+--..",
    r"                                                              .-++.      .++.+.",
    r"                                                             .-.++.     .-+.-+..",
    r"                                                            ....++-.... .#-.---#..",
    r"                                                           ..-..+#+....---..+-.....................",
    r"                                                          ..--.+.+#-+++--......#+-.....-+-+.--+-.--..",
    r"                                                       .......--.+##---.# .-#####...----++###+....-...",
    r"                                       ....................-+--..+++...   ......-.  ....-++...#+....",
    r"                                     ...+-++#######+###++#+++++##+-#       ......      ........",
    r"                                    ....---...++..-++--.... .--..+-.",
    r"                                       ......+..........    ........",
];

const SPLASH_QUOTES: &[&str] = &[
    "It works on my machine.",
    "// TODO: fix this later",
    "git push --force and pray.",
    "sudo make me a sandwich.",
    "The cloud is just someone else's computer.",
    "Works on my container.",
    "Have you tried turning it off and on again?",
    "It's not a bug, it's an undocumented feature.",
    "99 little bugs in the code...",
    "There is no place like 127.0.0.1.",
    "Real programmers count from 0.",
    "Deleted code is debugged code.",
    "The best code is no code at all.",
];

pub fn render_splash(frame: &mut Frame, area: Rect, _message: &str) {
    let w = area.width as usize;
    let h = area.height as usize;
    if w < 10 || h < 5 {
        return;
    }

    // Step 1: Strip common leading whitespace
    let min_indent = HEIHEI_ART
        .iter()
        .filter(|l| !l.trim().is_empty())
        .map(|l| l.len() - l.trim_start().len())
        .min()
        .unwrap_or(0);

    let trimmed: Vec<&str> = HEIHEI_ART
        .iter()
        .map(|l| {
            if l.len() > min_indent {
                &l[min_indent..]
            } else {
                ""
            }
        })
        .collect();

    let art_h = trimmed.len();
    let art_w = trimmed.iter().map(|l| l.len()).max().unwrap_or(0);

    // Step 2: Scale to 77% of terminal, preserving aspect ratio
    let target_w = (w as f64 * 0.77) as usize;
    let target_h = (h as f64 * 0.77) as usize;

    let scale_x = target_w as f64 / art_w as f64;
    let scale_y = target_h as f64 / art_h as f64;
    let mut scale = scale_x.min(scale_y);
    if scale > 1.0 {
        scale = 1.0;
    }

    let out_h = ((art_h as f64 * scale) as usize).max(1);
    let out_w = ((art_w as f64 * scale) as usize).max(1);

    // Step 3: Nearest-neighbor sampling
    let mut scaled: Vec<String> = Vec::with_capacity(out_h);
    for y in 0..out_h {
        let src_y = ((y as f64 / scale) as usize).min(art_h - 1);
        let src_line = trimmed[src_y];
        let mut buf = String::with_capacity(out_w);
        for x in 0..out_w {
            let src_x = (x as f64 / scale) as usize;
            if src_x < src_line.len() {
                buf.push(src_line.as_bytes()[src_x] as char);
            } else {
                buf.push(' ');
            }
        }
        scaled.push(buf);
    }

    // Step 4: Center and render
    let start_row = h.saturating_sub(out_h + 4) / 2;
    let start_col = w.saturating_sub(out_w) / 2;
    let pad = " ".repeat(start_col);

    // Art color: dim gray (240) matching Go
    let art_style = Style::default().fg(Color::Indexed(240));

    let mut lines: Vec<Line> = Vec::new();
    for _ in 0..start_row {
        lines.push(Line::from(""));
    }

    for line in &scaled {
        lines.push(Line::from(Span::styled(
            format!("{}{}", pad, line),
            art_style,
        )));
    }

    // Random quote — pick once per process using PID as seed
    let quote_idx = (std::process::id() as usize) % SPLASH_QUOTES.len();
    let quote = format!("\"{}\"", SPLASH_QUOTES[quote_idx]);

    lines.push(Line::from(""));
    let q_pad = w.saturating_sub(quote.len()) / 2;
    lines.push(Line::from(Span::styled(
        format!("{}{}", " ".repeat(q_pad), quote),
        Style::default().fg(HINT_COLOR).italic(),
    )));

    // Bottom message
    let msg = if _message.is_empty() {
        "Now that you found the easter egg, you owe me a beer! - @elvisnm"
    } else {
        _message
    };
    let m_pad = w.saturating_sub(msg.len()) / 2;
    lines.push(Line::from(Span::styled(
        format!("{}{}", " ".repeat(m_pad), msg),
        if _message.is_empty() { Style::default().fg(HINT_COLOR) } else { Style::default().fg(DIM_TEXT_COLOR) },
    )));

    // Version
    let ver = crate::version_label();
    let v_pad = w.saturating_sub(ver.len()) / 2;
    lines.push(Line::from(""));
    lines.push(Line::from(Span::styled(
        format!("{}{}", " ".repeat(v_pad), ver),
        Style::default().fg(DIM_TEXT_COLOR),
    )));

    let content = Paragraph::new(lines);
    frame.render_widget(content, area);
}
