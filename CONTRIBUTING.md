# Contributing

Thanks for helping. Bug reports and pull requests are welcome; for a security problem, see
[SECURITY.md](SECURITY.md) instead.

## Toolchain

Go 1.22 or later (the `go` directive is 1.22 and CI also runs oldstable and stable). The module
uses the standard library only; keep it that way. If Go is not installed, the official image
works:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.22 go test -race ./...
```

## Checks

Run what CI runs before you push:

```sh
gofmt -l .                                   # must print nothing
go vet ./... && go vet -tags contract ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go test -race ./...
grep -rn $'\xe2\x80\x94' . && echo "em dash found" # must find nothing
```

`staticcheck@latest` needs a recent Go; with an older one, set `GOTOOLCHAIN=auto` so the go
command fetches a toolchain for the tool.

Never use the em dash character anywhere (code, comments, docs, commit messages): rewrite the
sentence with commas, colons, full stops or parentheses.

## The conformance fixtures

`testdata/conformance/` is a vendored, unmodified copy of the `conformance/` directory of
[unicom360-openapi](https://github.com/TechRode-Solution-Limited/unicom360-openapi), the shared
contract every Unicom360 SDK follows. `go test` runs every vector of every file through the
public API, and `TestConformanceFixturesUnmodified` fails if the copy no longer matches the
sha256 in `testdata/conformance/VERSION`. Never edit the fixtures here: change them in
unicom360-openapi, tag a release there, then sync:

```sh
# from a checkout of unicom360-openapi at the new tag
scripts/sync-conformance ../unicom360-go/testdata/conformance
scripts/sync-conformance --check ../unicom360-go/testdata/conformance
```

Update `ContractVersion` in `version.go` to the new contract version in the same change.
`testdata/go.mod` keeps the fixtures out of the published module zip; the tests skip the
conformance suite only when the whole `testdata/` directory is absent (as in the module cache).

## The contract test

`contract_test.go` (build tag `contract`) sends every `requests.json` call to a
[Prism](https://github.com/stoplightio/prism) mock of `openapi.yaml` and fails on any
violation Prism reports:

```sh
docker run -d --rm --name prism -p 4010:4010 \
  -v "$PWD/../unicom360-openapi:/spec:ro" \
  stoplight/prism:5 mock -h 0.0.0.0 --multiprocess=false /spec/openapi.yaml
UNICOM360_CONTRACT_BASE_URL=http://127.0.0.1:4010 go test -tags contract -run Contract -count=1 ./...
```

When Go runs in a container too, share Prism's network namespace so `127.0.0.1` reaches it
(`docker run --network container:prism ... golang:1.22 go test -tags contract ...`). Plain
http to a loopback address is allowed by the client, so the test needs no special case.

## Releasing

1. Move the `## [x.y.z] - Unreleased` section of `CHANGELOG.md` to today's date, and check that
   `Version` in `version.go` matches (a test enforces it).
2. Commit, then tag and push the tag:

   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. `release.yml` runs the tests, creates the GitHub Release from the CHANGELOG section and asks
   the Go module proxy to fetch the new version. If that last step fails, warm the proxy by
   hand so `go get` and pkg.go.dev see the release:

   ```sh
   curl -sf https://proxy.golang.org/github.com/!tech!rode-!solution-!limited/unicom360-go/@v/v0.1.0.info
   ```

   (The proxy escapes each capital letter as `!` and the lower-case letter.)

A tag is permanent once the proxy has fetched it: never move or reuse one. Fix a bad release
with a new patch version (and, if needed, a `retract` directive in `go.mod`).

## Commits

Plain, descriptive commit messages; no AI attribution lines.
