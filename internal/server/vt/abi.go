package vt

// The parts of libghostty-vt's C API (include/ghostty/vt/*.h at the pinned
// version) that koko calls, as the WebAssembly module sees them: wasm32,
// so a pointer and a size_t are 4 bytes and a struct passed by value goes
// as a pointer to it.

// Results (types.h).
const (
	resultSuccess       = 0
	resultOutOfMemory   = -1
	resultInvalidValue  = -2
	resultOutOfSpace    = -3
	resultNoValue       = -4
	resultIOError       = -5
	resultLimitExceeded = -6
	resultUnsupported   = -8
)

// Terminal options, set with ghostty_terminal_set (terminal.h).
const (
	optWritePty           = 2
	optSize               = 7
	optColorForeground    = 12
	optColorBackground    = 13
	optKittyImageLimit    = 16
	optAPCMaxBytes        = 20
	optDefaultCursorStyle = 23
	optScrollbackMaxBytes = 28
	optScrollbackMaxLines = 29
	optContinuationMax    = 32
	optMode               = 35
)

// Terminal data, read with ghostty_terminal_get (terminal.h).
const (
	dataCols                   = 1
	dataRows                   = 2
	dataCursorX                = 3
	dataCursorY                = 4
	dataCursorPendingWrap      = 5
	dataActiveScreen           = 6
	dataKittyKeyboardFlags     = 8
	dataTitle                  = 12
	dataPwd                    = 13
	dataTotalRows              = 14
	dataScrollbackRows         = 15
	dataColorForeground        = 18
	dataColorBackground        = 19
	dataColorCursor            = 20
	dataColorPalette           = 21
	dataColorForegroundDefault = 22
	dataColorBackgroundDefault = 23
	dataColorCursorDefault     = 24
	dataColorPaletteDefault    = 25
	dataMode                   = 37
	dataVTGround               = 38
)

// screenAlternate is GHOSTTY_TERMINAL_SCREEN_ALTERNATE.
const screenAlternate = 1

// Cursor styles (types.h).
const (
	cursorBar   = 0
	cursorBlock = 1
)

// Formatter options and their values (formatter.h, types.h).
const (
	formatterOptEmit   = 1
	formatterOptUnwrap = 2
	formatterOptTrim   = 3
	formatterOptExtras = 5

	formatVT = 1
)

// Formatter extras (formatter.h).
const (
	extraModes           = 1 << 1
	extraScrollingRegion = 1 << 2
	extraTabstops        = 1 << 3
	extraKeyboard        = 1 << 5
	extraCursor          = 1 << 6
	extraStyle           = 1 << 7
	extraHyperlink       = 1 << 8
	extraProtection      = 1 << 9
	extraKittyKeyboard   = 1 << 10
	extraCharsets        = 1 << 11
)

// decMode is GhosttyMode for a DEC private mode (modes.h): its number,
// with bit 15, the ANSI flag, clear.
func decMode(n uint16) uint16 { return n & 0x7fff }

// The alternate screen's modes, in the order a core is asked which one
// entered the alternate screen (Snapshot).
var altScreenModes = []uint16{1049, 1047, 47}
