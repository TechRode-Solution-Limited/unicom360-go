// This file keeps testdata/ (the vendored conformance fixtures) out of the published module
// zip: the go command leaves out every directory that holds its own go.mod. The tests read the
// fixtures straight from disk, so they still run in a checkout of the repository.
module github.com/TechRode-Solution-Limited/unicom360-go/testdata

go 1.22
