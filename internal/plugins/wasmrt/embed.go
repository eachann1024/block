package wasmrt

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

// ExpectedQJSSHA256 is the SHA-256 hex digest of official quickjs-ng v0.18.0 qjs-wasi.wasm.
const ExpectedQJSSHA256 = "2c218412cb8b17e46ee91bea7556ec38f5b9b6bd6312fddd3ceeed14aa71eaaa"

//go:embed qjs-wasi.wasm.gz
var qjsWasmGZ []byte

//go:embed block-sdk.js
var runtimeEmbedFS embed.FS

var (
	qjsWasmOnce sync.Once
	qjsWasmData []byte
	qjsWasmErr  error
)

// LoadQJSWasm decompresses the embedded qjs-wasi.wasm binary and verifies its SHA-256 digest.
func LoadQJSWasm() ([]byte, error) {
	qjsWasmOnce.Do(func() {
		zr, err := gzip.NewReader(bytes.NewReader(qjsWasmGZ))
		if err != nil {
			qjsWasmErr = fmt.Errorf("open embedded qjs-wasi.wasm.gz: %w", err)
			return
		}
		defer zr.Close()
		raw, err := io.ReadAll(zr)
		if err != nil {
			qjsWasmErr = fmt.Errorf("decompress embedded qjs-wasi.wasm.gz: %w", err)
			return
		}
		sum := sha256.Sum256(raw)
		got := hex.EncodeToString(sum[:])
		if got != ExpectedQJSSHA256 {
			qjsWasmErr = fmt.Errorf("embedded qjs-wasi.wasm sha256 mismatch: got %s want %s", got, ExpectedQJSSHA256)
			return
		}
		qjsWasmData = raw
	})
	return qjsWasmData, qjsWasmErr
}
