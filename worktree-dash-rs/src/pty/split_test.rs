#[cfg(test)]
mod tests {
    use crate::pty::split::{SplitDir, SplitNode, DotMapSpan};

    #[test]
    fn leaf_count_single() {
        let node = SplitNode::leaf(1);
        assert_eq!(node.leaf_count(), 1);
    }

    #[test]
    fn leaf_count_split() {
        let node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        assert_eq!(node.leaf_count(), 2);
    }

    #[test]
    fn session_ids_collects_all() {
        let node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(10),
            SplitNode::split(
                SplitDir::Vertical,
                SplitNode::leaf(20),
                SplitNode::leaf(30),
            ),
        );
        let ids = node.session_ids();
        assert_eq!(ids, vec![10, 20, 30]);
    }

    #[test]
    fn first_leaf() {
        let node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::split(SplitDir::Vertical, SplitNode::leaf(5), SplitNode::leaf(6)),
            SplitNode::leaf(7),
        );
        assert_eq!(node.first_leaf(), 5);
    }

    #[test]
    fn add_split_same_direction_flat() {
        // H(1,2) + add_split(3, H) → H(1,2,3)
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        node.add_split(3, SplitDir::Horizontal);
        assert_eq!(node.leaf_count(), 3);
        assert_eq!(node.session_ids(), vec![1, 2, 3]);

        // Should be flat: H(1,2,3) not H(1,H(2,3))
        if let SplitNode::Split { children, .. } = &node {
            assert_eq!(children.len(), 3);
        } else {
            panic!("expected Split");
        }
    }

    #[test]
    fn add_split_different_direction_nests() {
        // H(1,2) + add_split(3, V) → H(1, V(2,3))
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        node.add_split(3, SplitDir::Vertical);
        assert_eq!(node.leaf_count(), 3);
        assert_eq!(node.session_ids(), vec![1, 2, 3]);
    }

    #[test]
    fn find_and_split_creates_at_target() {
        // H(1,2) → find_and_split(2, 3, H) → H(1,2,3) (sibling, same dir)
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        assert!(node.find_and_split(2, 3, SplitDir::Horizontal));
        assert_eq!(node.session_ids(), vec![1, 2, 3]);
        assert_eq!(node.leaf_count(), 3);
    }

    #[test]
    fn find_and_split_different_dir() {
        // H(1,2) → find_and_split(2, 3, V) → H(1, V(2,3))
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        assert!(node.find_and_split(2, 3, SplitDir::Vertical));
        assert_eq!(node.leaf_count(), 3);
        assert_eq!(node.session_ids(), vec![1, 2, 3]);
    }

    #[test]
    fn find_and_split_target_not_found() {
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        assert!(!node.find_and_split(99, 3, SplitDir::Horizontal));
        assert_eq!(node.leaf_count(), 2);
    }

    #[test]
    fn remove_session_from_split() {
        let mut node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::leaf(1),
            SplitNode::leaf(2),
        );
        let empty = node.remove_session(1);
        assert!(!empty);
        // Should collapse to single leaf
        assert_eq!(node.leaf_count(), 1);
        assert_eq!(node.first_leaf(), 2);
    }

    #[test]
    fn remove_last_session() {
        let mut node = SplitNode::leaf(1);
        let empty = node.remove_session(1);
        assert!(empty);
    }

    #[test]
    fn remove_from_three_children() {
        // H(1,2,3) → remove 2 → H(1,3)
        let mut node = SplitNode::Split {
            direction: SplitDir::Horizontal,
            children: vec![SplitNode::leaf(1), SplitNode::leaf(2), SplitNode::leaf(3)],
        };
        node.remove_session(2);
        assert_eq!(node.leaf_count(), 2);
        assert_eq!(node.session_ids(), vec![1, 3]);
    }

    #[test]
    fn dot_map_single_leaf_empty() {
        let node = SplitNode::leaf(1);
        assert!(node.dot_map(None).is_empty());
    }

    #[test]
    fn dot_map_horizontal_split() {
        let node = SplitNode::split(SplitDir::Horizontal, SplitNode::leaf(1), SplitNode::leaf(2));
        let map = node.dot_map(None);
        assert_eq!(map.len(), 1); // 1 row
        assert_eq!(map[0].spans.len(), 3); // dot, space, dot
    }

    #[test]
    fn dot_map_vertical_split() {
        let node = SplitNode::split(SplitDir::Vertical, SplitNode::leaf(1), SplitNode::leaf(2));
        let map = node.dot_map(None);
        assert_eq!(map.len(), 2); // 2 rows
    }

    #[test]
    fn dot_map_highlight() {
        let node = SplitNode::split(SplitDir::Horizontal, SplitNode::leaf(1), SplitNode::leaf(2));
        let map = node.dot_map(Some(2));
        // First dot should not be highlighted, second should be
        let dots: Vec<bool> = map[0].spans.iter().filter_map(|s| match s {
            DotMapSpan::Dot { highlighted } => Some(*highlighted),
            _ => None,
        }).collect();
        assert_eq!(dots, vec![false, true]);
    }

    #[test]
    fn dot_map_2x2_grid() {
        // H(V(1,2), V(3,4)) → 2 rows, 2 cols
        let node = SplitNode::split(
            SplitDir::Horizontal,
            SplitNode::split(SplitDir::Vertical, SplitNode::leaf(1), SplitNode::leaf(2)),
            SplitNode::split(SplitDir::Vertical, SplitNode::leaf(3), SplitNode::leaf(4)),
        );
        let map = node.dot_map(None);
        assert_eq!(map.len(), 2); // 2 rows
        let dots_per_row: Vec<usize> = map.iter().map(|row| {
            row.spans.iter().filter(|s| matches!(s, DotMapSpan::Dot { .. })).count()
        }).collect();
        assert_eq!(dots_per_row, vec![2, 2]); // 2 dots per row
    }
}
