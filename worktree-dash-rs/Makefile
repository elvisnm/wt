.PHONY: dev build clean run

# Dev build — named wt-dev to avoid conflicting with Homebrew binary
dev:
	cargo build
	cp target/debug/wt wt-dev
	@echo "Built: worktree-dash-rs/wt-dev"

# Release build
build:
	cargo build --release
	cp target/release/wt wt-dev
	@echo "Built (release): worktree-dash-rs/wt-dev"

# Run dev build directly
run: dev
	./wt-dev

clean:
	cargo clean
	rm -f wt-dev
