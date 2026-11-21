# Developing ykode

The rules for changing the code are in [`AGENTS.md`](../AGENTS.md).

## Make targets

`make check` is the gate (vet, gofmt, tests, lint), `make audit` reports dependency updates and known
CVEs through Trivy in Docker, `make release` cross-builds the three shipped targets into `dist/`, and
`make install-hooks` makes git check your commit subjects and tag names.

## Evals

`make eval` runs scripted tasks against a real server, three runs each, and saves the result under
`~/.ykode/evals`: pass rate, median turns, prompt and generated tokens and seconds per task, and how often
a run stalled, folded, dropped or repeated itself. `ARGS` takes its own flags (`--only NAME`, `--repeat N`,
`--baseline last` to compare with the previous run, `--keep`, `--verbose`, `--use-settings`) and ykode's,
so `make eval ARGS="--thinking xhigh --baseline last"` measures one level against another. It reads an
empty home unless told `--use-settings`, so your settings and `AGENTS.md` change nothing it measures.

## Releasing

A release is a `vX.Y.Z` tag, and it is prepared in a commit before the tag: move the `[Unreleased]`
entries of `CHANGELOG.md` under a new `[X.Y.Z] - YYYY-MM-DD` heading and start `[Unreleased]` again.
Then `git tag vX.Y.Z && git push origin vX.Y.Z`. CD re-runs the CI gate, refuses a tag that is not
semver or has no `CHANGELOG.md` section, and publishes the three binaries with their `SHA256SUMS` and
cosign signatures as a GitHub Release whose notes are that changelog section. There is no `:latest`
and no rolling artifact: a release is a tag, and the newest tag is the newest release.
