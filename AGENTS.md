# SimpleToggle

macOS menu bar manager that turns scripts into switches, written in Go with mygo (native GPU UI). See `README.md` and `docs/architecture.md`.

## Layout

| Path | What |
|---|---|
| `app/` | the UI |
| `cmd/simpletoggle/` | the CLI |
| `internal/` | core packages |
| `skills/` | the agent skill linked by `~/dotfiles/install.sh` |
| `Scripts/release.sh` | release: needs the signing identity, notarization profile and update key |

## Commands

```bash
go tool mygo dev                     # run with live reload
gofmt -l app cmd internal            # must print nothing
go vet ./...
go test -race -count=1 ./...
go build -o /tmp/simpletoggle ./cmd/simpletoggle
```

## Rules

- `main` only changes through a PR. The public cask lives in `HanlunWang/homebrew-tap`, which also takes PRs only.
- Never run `go tool mygo build` or `Scripts/release.sh` without the owner's go-ahead: they sign and publish.
- Switch scripts follow the contract in `skills/` and must pass `simpletoggle validate`.
