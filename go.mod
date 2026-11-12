module github.com/cieons/koko

go 1.27

require (
	github.com/coder/websocket v1.8.15
	golang.org/x/crypto v0.58.0
	golang.org/x/mod v0.42.0
	golang.org/x/sys v0.49.0
	golang.org/x/term v0.47.0
	golang.org/x/text v0.43.0
	modernc.org/sqlite v1.61.1
)

require (
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	// modernc.org/libc stays at the version modernc.org/sqlite's go.mod
	// requires: upgrade the two together (TestLibcIsTheVersionSQLitePins).
	modernc.org/libc v1.79.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
