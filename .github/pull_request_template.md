## What this changes

<!-- What behaviour is different after this lands, in a sentence or two.
     If it fixes an issue: Fixes #123 -->

## Why

<!-- The problem, not the patch. If a measurement or a failing case drove
     this, put the numbers or the input here — that is what makes the change
     reviewable. -->

## Checks

- [ ] `go build ./...` (or the affected packages — see CLAUDE.md for the
      per-binary Windows build)
- [ ] `go vet` clean on the packages touched
- [ ] `go test ./...` on the packages touched; `-race` if goroutines are
      involved
- [ ] CHANGELOG.md updated, under `[Unreleased]` unless this is a release
- [ ] Docs updated if a flag, a config key or the packet format changed

## Compatibility

<!-- Delete what does not apply.

     - Packet format: does an older reader still read what this writes?
     - Config/CLI: any flag or YAML key renamed or removed?
     - Database: any schema change, and does it migrate an existing file?
     - Operations: anything an operator must do at deploy time (new files
       beside a database, a new required flag, a changed default)?
-->

None.

## Anything a reviewer should look at first

<!-- Optional. The part you are least sure about, or the decision worth
     arguing with. -->
