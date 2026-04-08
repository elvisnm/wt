#[cfg(test)]
#[path = "split_test.rs"]
mod tests;

/// Direction for a split.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SplitDir {
    Horizontal, // side-by-side
    Vertical,   // stacked
}

/// A node in the binary split tree.
#[derive(Debug, Clone)]
pub enum SplitNode {
    Leaf(usize),
    Split {
        direction: SplitDir,
        children: Vec<SplitNode>,
    },
}

impl SplitNode {
    pub fn leaf(session_id: usize) -> Self {
        SplitNode::Leaf(session_id)
    }

    pub fn split(direction: SplitDir, left: SplitNode, right: SplitNode) -> Self {
        SplitNode::Split {
            direction,
            children: vec![left, right],
        }
    }

    pub fn leaf_count(&self) -> usize {
        match self {
            SplitNode::Leaf(_) => 1,
            SplitNode::Split { children, .. } => {
                children.iter().map(|c| c.leaf_count()).sum()
            }
        }
    }

    pub fn first_leaf(&self) -> usize {
        match self {
            SplitNode::Leaf(id) => *id,
            SplitNode::Split { children, .. } => children[0].first_leaf(),
        }
    }

    pub fn session_ids(&self) -> Vec<usize> {
        match self {
            SplitNode::Leaf(id) => vec![*id],
            SplitNode::Split { children, .. } => {
                children.iter().flat_map(|c| c.session_ids()).collect()
            }
        }
    }

    pub fn add_split(&mut self, session_id: usize, direction: SplitDir) {
        match self {
            SplitNode::Leaf(existing_id) => {
                let existing = SplitNode::Leaf(*existing_id);
                let new = SplitNode::Leaf(session_id);
                *self = SplitNode::Split {
                    direction,
                    children: vec![existing, new],
                };
            }
            SplitNode::Split { children, direction: dir } => {
                if *dir == direction {
                    children.push(SplitNode::Leaf(session_id));
                } else {
                    if let Some(last) = children.last_mut() {
                        last.add_split(session_id, direction);
                    }
                }
            }
        }
    }

    /// Find a specific target session and split it: target goes left/top, new goes right/bottom.
    /// If the parent split has the same direction, inserts as sibling (flat: H(1,2,3) not H(1,H(2,3))).
    /// Returns true if the target was found and split.
    pub fn find_and_split(&mut self, target_id: usize, new_id: usize, direction: SplitDir) -> bool {
        match self {
            SplitNode::Leaf(id) if *id == target_id => {
                let existing = SplitNode::Leaf(target_id);
                let new = SplitNode::Leaf(new_id);
                *self = SplitNode::Split {
                    direction,
                    children: vec![existing, new],
                };
                true
            }
            SplitNode::Leaf(_) => false,
            SplitNode::Split { direction: dir, children } => {
                // If same direction, check if target is a direct child leaf — add as sibling
                if *dir == direction {
                    if let Some(pos) = children.iter().position(|c| matches!(c, SplitNode::Leaf(id) if *id == target_id)) {
                        // Insert new leaf right after the target
                        children.insert(pos + 1, SplitNode::Leaf(new_id));
                        return true;
                    }
                }
                // Recurse into children
                for child in children.iter_mut() {
                    if child.find_and_split(target_id, new_id, direction) {
                        return true;
                    }
                }
                false
            }
        }
    }

    pub fn remove_session(&mut self, session_id: usize) -> bool {
        match self {
            SplitNode::Leaf(id) => *id == session_id,
            SplitNode::Split { children, .. } => {
                children.retain_mut(|child| !child.remove_session(session_id));
                if children.is_empty() {
                    return true;
                }
                if children.len() == 1 {
                    *self = children.remove(0);
                }
                false
            }
        }
    }

    /// Generate a dot map with ● characters, optionally highlighting one session.
    /// Returns lines like "● ●" or "● ●\n● ●" for a 2x2 grid.
    pub fn dot_map(&self, highlight_id: Option<usize>) -> Vec<DotMapLine> {
        if matches!(self, SplitNode::Leaf(_)) {
            return vec![];
        }

        // Assign (col, row) positions to each leaf
        let mut positions: Vec<(usize, usize, usize)> = Vec::new(); // (session_id, col, row)
        assign_positions(self, 0, 0, &mut positions);

        if positions.is_empty() {
            return vec![];
        }

        let max_col = positions.iter().map(|p| p.1).max().unwrap_or(0);
        let max_row = positions.iter().map(|p| p.2).max().unwrap_or(0);

        // Build grid
        let mut grid = vec![vec![0usize; max_col + 1]; max_row + 1];
        for &(sid, col, row) in &positions {
            grid[row][col] = sid;
        }

        // Render
        let mut lines = Vec::new();
        for row in &grid {
            let mut dots = Vec::new();
            for (c, &sid) in row.iter().enumerate() {
                if c > 0 {
                    dots.push(DotMapSpan::Space);
                }
                if sid > 0 {
                    let highlighted = highlight_id == Some(sid);
                    dots.push(DotMapSpan::Dot { highlighted });
                } else {
                    dots.push(DotMapSpan::Space);
                }
            }
            lines.push(DotMapLine { spans: dots });
        }
        lines
    }
}

#[derive(Debug, Clone)]
pub enum DotMapSpan {
    Dot { highlighted: bool },
    Space,
}

#[derive(Debug, Clone)]
pub struct DotMapLine {
    pub spans: Vec<DotMapSpan>,
}

fn assign_positions(node: &SplitNode, col: usize, row: usize, out: &mut Vec<(usize, usize, usize)>) {
    match node {
        SplitNode::Leaf(id) => {
            out.push((*id, col, row));
        }
        SplitNode::Split { direction, children } => {
            match direction {
                SplitDir::Horizontal => {
                    let mut c = col;
                    for child in children {
                        assign_positions(child, c, row, out);
                        c += count_h_leaves(child);
                    }
                }
                SplitDir::Vertical => {
                    let mut r = row;
                    for child in children {
                        assign_positions(child, col, r, out);
                        r += count_v_leaves(child);
                    }
                }
            }
        }
    }
}

fn count_h_leaves(node: &SplitNode) -> usize {
    match node {
        SplitNode::Leaf(_) => 1,
        SplitNode::Split { direction: SplitDir::Horizontal, children } => {
            children.iter().map(|c| count_h_leaves(c)).sum()
        }
        SplitNode::Split { direction: SplitDir::Vertical, children } => {
            children.iter().map(|c| count_h_leaves(c)).max().unwrap_or(1)
        }
    }
}

fn count_v_leaves(node: &SplitNode) -> usize {
    match node {
        SplitNode::Leaf(_) => 1,
        SplitNode::Split { direction: SplitDir::Vertical, children } => {
            children.iter().map(|c| count_v_leaves(c)).sum()
        }
        SplitNode::Split { direction: SplitDir::Horizontal, children } => {
            children.iter().map(|c| count_v_leaves(c)).max().unwrap_or(1)
        }
    }
}

/// A tab group that may contain a single session or a split layout.
#[derive(Debug, Clone)]
pub struct TabGroup {
    pub root: SplitNode,
}

impl TabGroup {
    pub fn single(session_id: usize) -> Self {
        Self { root: SplitNode::leaf(session_id) }
    }

    pub fn session_ids(&self) -> Vec<usize> {
        self.root.session_ids()
    }

    pub fn add_split(&mut self, session_id: usize, direction: SplitDir) {
        self.root.add_split(session_id, direction);
    }

    pub fn remove_session(&mut self, session_id: usize) -> bool {
        self.root.remove_session(session_id)
    }

    pub fn dot_map(&self, highlight_id: Option<usize>) -> Vec<DotMapLine> {
        self.root.dot_map(highlight_id)
    }

    pub fn size(&self) -> usize {
        self.root.leaf_count()
    }

    /// Keep the old layout_map for backwards compat (returns string lines)
    pub fn layout_map(&self) -> Vec<String> {
        let lines = self.dot_map(None);
        lines.iter().map(|l| {
            l.spans.iter().map(|s| match s {
                DotMapSpan::Dot { .. } => "●",
                DotMapSpan::Space => " ",
            }).collect()
        }).collect()
    }
}
