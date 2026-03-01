# GoReleaser + Release Workflow Design

Date: 2026-03-01

## Problem

- `install.sh`, `.goreleaser.yaml`, and `selfupdate.go` reference repo name `shannon-cli` but actual GitHub repo is `Kocoro-lab/shan`
- No GitHub Actions workflow exists to automate releases
- No Homebrew tap for `brew install shan`

## Design

### 1. Fix repo name references

| File | Old | New |
|------|-----|-----|
| `.goreleaser.yaml` release section | `name: shannon-cli` | `name: shan` |
| `internal/update/selfupdate.go` | `repoName = "shannon-cli"` | `repoName = "shan"` |
| `install.sh` | `REPO="Kocoro-lab/shannon-cli"` | `REPO="Kocoro-lab/shan"` (done) |

### 2. `.goreleaser.yaml` updates

- Fix `release.github.name` to `shan`
- Add `brews` section targeting `Kocoro-lab/homebrew-tap`
- Formula name: `shan`
- Keep: CGO_ENABLED=0, ldflags version injection, darwin+linux × amd64+arm64, tar.gz, checksums

### 3. `.github/workflows/release.yml`

- Trigger: `push tags v*`
- Single job on `ubuntu-latest`
- Steps: checkout → setup-go 1.25 → goreleaser-action v6
- Env: `GITHUB_TOKEN` for release, `HOMEBREW_TAP_TOKEN` for tap push

### 4. Prerequisites (manual)

- Create `Kocoro-lab/homebrew-tap` repo on GitHub (empty, public)
- Create PAT (classic) with `repo` scope
- Add as `HOMEBREW_TAP_TOKEN` secret in `Kocoro-lab/shan` repo settings

### 5. Release flow

```
git tag v0.1.0
git push origin v0.1.0
→ GitHub Actions triggers
→ GoReleaser builds darwin/linux × amd64/arm64
→ Creates GitHub release with archives + checksums
→ Pushes Formula/shan.rb to homebrew-tap repo
```

### 6. Install methods after first release

- `curl -fsSL https://raw.githubusercontent.com/Kocoro-lab/shan/main/install.sh | sh`
- `brew tap Kocoro-lab/tap && brew install shan`
- `brew install Kocoro-lab/tap/shan` (one-liner)
- `/update` command in shan TUI (self-update via go-selfupdate)
