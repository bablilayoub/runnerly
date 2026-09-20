// This module exists only to keep the Go tool out of web/.
//
// npm packages sometimes vendor Go source — eslint's `flatted` ships a
// package under node_modules/flatted/golang. Without a module boundary here,
// `go build ./...` and `go test ./...` from the repository root compile and
// lint third-party code that has nothing to do with Runnerly, and a broken
// file in someone else's package would break this build.
//
// Nothing here is imported. The dashboard's build output goes to
// internal/web/dist, which is part of the real module.
module github.com/bablilayoub/runnerly/web

go 1.25
