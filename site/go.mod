// This directory holds no Go code. The module boundary exists because npm
// packages occasionally vendor Go source, and without it `go build ./...`
// from the repository root would try to compile a dependency's Go files as
// part of Runnerly. web/ carries one for the same reason.
module github.com/bablilayoub/runnerly/site

go 1.25.0
