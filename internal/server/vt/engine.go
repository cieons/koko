package vt

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// wasm is libghostty-vt's WebAssembly module, as released: the file
// ghostty-vt.wasm.sum names, pinned by its SHA-256 there, which loadEngine
// checks (make vt-wasm fetches the file again).
//
//go:embed ghostty-vt.wasm
var wasm []byte

//go:embed ghostty-vt.wasm.sum
var wasmSum string

// memoryLimitPages caps each instance's linear memory, in 64 KiB pages:
// 128 MiB, memoryBound and more than any one call has been seen to add,
// about 8 MiB for a resize. A core fails at memoryBound after the call
// that passed it, so Ghostty's own allocations do not fail at the cap,
// which would leave its state inconsistent.
const memoryLimitPages = 2048

// engine is the process's one wazero runtime, with Ghostty's module and
// the bridge compiled into it once; each Terminal is an instance of both.
type engine struct {
	runtime wazero.Runtime
	ghostty wazero.CompiledModule
	bridge  wazero.CompiledModule
	kind    string // which of wazero's engines runs the module, for Engine
}

// loadEngine checks the module against its pin and compiles it on first
// use, about 150 ms, and gives every later caller the same engine.
var loadEngine = sync.OnceValues(func() (*engine, error) {
	if err := checkPin(wasm); err != nil {
		return nil, err
	}
	// wazero's compiler writes machine code to memory and then executes
	// it. Where the system refuses that, as MemoryDenyWriteExecute= does,
	// the interpreter runs the module, about 100 times slower.
	e, err := compile(wazero.NewRuntimeConfigCompiler())
	if err == nil {
		e.kind = "compiler"
		return e, nil
	}
	e, ierr := compile(wazero.NewRuntimeConfigInterpreter())
	if ierr != nil {
		return nil, errors.Join(err, ierr)
	}
	e.kind = fmt.Sprintf("interpreter, since the compiler failed: %v", err)
	return e, nil
})

// checkPin checks a module against the SHA-256 ghostty-vt.wasm.sum pins.
func checkPin(module []byte) error {
	want, file, ok := strings.Cut(strings.TrimSpace(wasmSum), "  ")
	if !ok {
		return fmt.Errorf("libghostty-vt: ghostty-vt.wasm.sum reads %q, not \"<sha256>  <release file>\"", wasmSum)
	}
	if got := sha256.Sum256(module); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("libghostty-vt: the embedded module is not %s, whose SHA-256 is pinned", file)
	}
	return nil
}

// compile makes a runtime of config with the modules compiled into it.
func compile(config wazero.RuntimeConfig) (*engine, error) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, config.WithMemoryLimitPages(memoryLimitPages))
	fail := func(err error) (*engine, error) {
		_ = runtime.Close(ctx)
		return nil, err
	}
	ghostty, err := runtime.CompileModule(ctx, wasm)
	if err != nil {
		return fail(fmt.Errorf("compile libghostty-vt: %w", err))
	}
	if err := instantiateHost(ctx, runtime); err != nil {
		return fail(err)
	}
	bridge, err := runtime.CompileModule(ctx, bridgeModule())
	if err != nil {
		return fail(fmt.Errorf("compile the bridge: %w", err))
	}
	return &engine{runtime: runtime, ghostty: ghostty, bridge: bridge}, nil
}

// Engine loads the emulator, if no Terminal has yet, and says which of
// wazero's engines runs it: "compiler", or "interpreter" and why, for the
// koko server to log once at its start.
func Engine() (string, error) {
	e, err := loadEngine()
	if err != nil {
		return "", err
	}
	return e.kind, nil
}

// terminalKey is the context key under which each call into a core
// carries its Terminal, for the callbacks the core makes during it.
type terminalKey struct{}

func terminalOf(ctx context.Context) *Terminal { return ctx.Value(terminalKey{}).(*Terminal) }

const i32 = api.ValueTypeI32

// instantiateHost gives the runtime the module "koko", whose functions
// are the callbacks koko hands Ghostty: the bridge puts them in Ghostty's
// function table. An address past the instance's memory panics in them,
// which wazero returns as the call's error.
func instantiateHost(ctx context.Context, runtime wazero.Runtime) error {
	_, err := runtime.NewHostModuleBuilder("koko").
		// write_pty(terminal, userdata, data, len): a reply to a query.
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, _ api.Module, stack []uint64) {
		t := terminalOf(ctx)
		t.replies = append(t.replies, t.read(uint32(stack[2]), uint32(stack[3]))...)
	}), []api.ValueType{i32, i32, i32, i32}, nil).Export("write_pty").
		// size(terminal, userdata, out *GhosttySizeReportSize) bool: the
		// pty's size for XTWINOPS, in cells, and 0 pixels, as the pty has.
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, _ api.Module, stack []uint64) {
		t := terminalOf(ctx)
		out := make([]byte, 12)
		binary.LittleEndian.PutUint16(out[0:], uint16(t.rows))
		binary.LittleEndian.PutUint16(out[2:], uint16(t.cols))
		t.write(uint32(stack[2]), out)
		stack[0] = 1
	}), []api.ValueType{i32, i32, i32}, []api.ValueType{i32}).Export("size").
		// write(userdata, data, len) bool: a GhosttyWriter's output.
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, _ api.Module, stack []uint64) {
		t := terminalOf(ctx)
		t.sink = append(t.sink, t.read(uint32(stack[1]), uint32(stack[2]))...)
		stack[0] = 1
	}), []api.ValueType{i32, i32, i32}, []api.ValueType{i32}).Export("write").
		// read(userdata, buffer, capacity, out_read *size_t) bool: a
		// GhosttyReader's input, from source.
		NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, _ api.Module, stack []uint64) {
		t := terminalOf(ctx)
		n := min(int(uint32(stack[2])), len(t.source))
		t.write(uint32(stack[1]), t.source[:n])
		t.source = t.source[n:]
		t.write(uint32(stack[3]), u32(uint32(n)))
		stack[0] = 1
	}), []api.ValueType{i32, i32, i32, i32}, []api.ValueType{i32}).Export("read").
		Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("instantiate koko's callbacks: %w", err)
	}
	return nil
}

// The bridge's callbacks, in the order install puts them in the table
// from the index it returns.
const (
	callbackWritePty = iota
	callbackSize
	callbackWrite
	callbackRead
	callbackCount
)

// bridgeModule is a WebAssembly module of koko's own, a few dozen bytes:
// Ghostty takes its callbacks as indices in its function table, and a Go
// function can reach that table only through a module that imports both.
// It imports Ghostty's table and koko's four callbacks, and exports
// install, which grows the table by four, puts the callbacks there, and
// returns the first one's index. It is written out here rather than
// compiled from text, since koko's build has no WebAssembly toolchain.
func bridgeModule() []byte {
	const (
		typeFunc     = 0x60
		typeI32      = 0x7f
		refFuncref   = 0x70
		importFunc   = 0x00
		importTable  = 0x01
		exportFunc   = 0x00
		opLocalGet   = 0x20
		opLocalSet   = 0x21
		opI32Const   = 0x41
		opI32Add     = 0x6a
		opRefNull    = 0xd0
		opRefFunc    = 0xd2
		opTableSet   = 0x26
		opPrefixFC   = 0xfc
		opTableGrow  = 0x0f
		opEnd        = 0x0b
		elemDeclared = 0x03
	)
	types := [][]byte{
		{typeFunc, 4, typeI32, typeI32, typeI32, typeI32, 0},          // 0: write_pty
		{typeFunc, 3, typeI32, typeI32, typeI32, 1, typeI32},          // 1: size, write
		{typeFunc, 4, typeI32, typeI32, typeI32, typeI32, 1, typeI32}, // 2: read
		{typeFunc, 0, 1, typeI32},                                     // 3: install
	}
	imports := [][]byte{
		cat(wasmName("ghostty"), wasmName("__indirect_function_table"), []byte{importTable, refFuncref, 0, 0}),
		cat(wasmName("koko"), wasmName("write_pty"), []byte{importFunc, 0}),
		cat(wasmName("koko"), wasmName("size"), []byte{importFunc, 1}),
		cat(wasmName("koko"), wasmName("write"), []byte{importFunc, 1}),
		cat(wasmName("koko"), wasmName("read"), []byte{importFunc, 2}),
	}
	// install's body, with one i32 local, base:
	//   base = table.grow(ref.null func, 4)
	//   table[base+i] = ref.func i, for each callback i
	//   return base
	body := []byte{1, 1, typeI32, opRefNull, refFuncref, opI32Const, callbackCount, opPrefixFC, opTableGrow, 0, opLocalSet, 0}
	var callbacks []byte // the imported functions' indices
	for i := range byte(callbackCount) {
		callbacks = append(callbacks, i)
		body = append(body, opLocalGet, 0, opI32Const, i, opI32Add, opRefFunc, i, opTableSet, 0)
	}
	body = append(body, opLocalGet, 0, opEnd)
	return cat(
		[]byte{0x00, 'a', 's', 'm', 1, 0, 0, 0},
		wasmSection(1, wasmVector(types...)),
		wasmSection(2, wasmVector(imports...)),
		wasmSection(3, wasmVector([]byte{3})),
		wasmSection(7, wasmVector(cat(wasmName("install"), []byte{exportFunc, callbackCount}))),
		wasmSection(9, wasmVector(cat([]byte{elemDeclared, 0x00, callbackCount}, callbacks))),
		wasmSection(10, wasmVector(cat(leb(len(body)), body))),
	)
}

func wasmSection(id byte, body []byte) []byte { return cat([]byte{id}, leb(len(body)), body) }

func wasmVector(items ...[]byte) []byte { return cat(append([][]byte{leb(len(items))}, items...)...) }

func wasmName(s string) []byte { return cat(leb(len(s)), []byte(s)) }

// leb is n as an unsigned LEB128.
func leb(n int) []byte {
	var b []byte
	for {
		c := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			return append(b, c)
		}
		b = append(b, c|0x80)
	}
}

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
