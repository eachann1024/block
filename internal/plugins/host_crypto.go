package plugins

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"strings"
)

func (rt *Runtime) dispatchCryptoHostMethod(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "host.crypto.hash":
		return rt.cryptoHash(params)
	case "host.crypto.hmac":
		return rt.cryptoHMAC(params)
	default:
		return nil, rpcErr(-32601, "unknown host method: "+method)
	}
}

func newCryptoHash(alg string, allowMD5 bool) (hash.Hash, string, bool) {
	switch strings.ToLower(strings.TrimSpace(alg)) {
	case "sha1":
		return sha1.New(), "sha1", true
	case "sha256":
		return sha256.New(), "sha256", true
	case "sha512":
		return sha512.New(), "sha512", true
	case "md5":
		if !allowMD5 {
			return nil, "", false
		}
		return md5.New(), "md5", true
	default:
		return nil, "", false
	}
}

func (rt *Runtime) cryptoHash(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Algorithm string  `json:"algorithm"`
		Text      *string `json:"text"`
		Base64    *string `json:"base64"`
		FilePath  *string `json:"file_path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErr(-32602, "invalid host.crypto.hash params: "+err.Error())
	}
	h, alg, ok := newCryptoHash(p.Algorithm, true)
	if !ok {
		return nil, rpcErr(-32602, "algorithm must be one of sha1, sha256, sha512, md5")
	}

	inputs := 0
	if p.Text != nil {
		inputs++
	}
	if p.Base64 != nil {
		inputs++
	}
	if p.FilePath != nil {
		inputs++
	}
	if inputs != 1 {
		return nil, rpcErr(-32602, "host.crypto.hash requires exactly one of text, base64, or file_path")
	}

	var size int64
	switch {
	case p.FilePath != nil:
		f, _, root, _, rerr := rt.openFSReadFile(*p.FilePath)
		if rerr != nil {
			return nil, rerr
		}
		n, err := io.Copy(h, f)
		_ = f.Close()
		_ = root.Close()
		if err != nil {
			return nil, rpcErr(-32004, err.Error())
		}
		size = n
	case p.Base64 != nil:
		data, err := base64.StdEncoding.DecodeString(*p.Base64)
		if err != nil {
			return nil, rpcErr(-32602, "invalid base64 input: "+err.Error())
		}
		_, _ = h.Write(data)
		size = int64(len(data))
	case p.Text != nil:
		data := []byte(*p.Text)
		_, _ = h.Write(data)
		size = int64(len(data))
	}

	sum := h.Sum(nil)
	return map[string]any{
		"algorithm": alg,
		"hex":       hex.EncodeToString(sum),
		"base64":    base64.StdEncoding.EncodeToString(sum),
		"size":      size,
	}, nil
}

func (rt *Runtime) cryptoHMAC(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Algorithm  string  `json:"algorithm"`
		KeyText    *string `json:"key_text"`
		KeyBase64  *string `json:"key_base64"`
		DataText   *string `json:"data_text"`
		DataBase64 *string `json:"data_base64"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, rpcErr(-32602, "invalid host.crypto.hmac params: "+err.Error())
	}
	algNorm := strings.ToLower(strings.TrimSpace(p.Algorithm))
	var hashFn func() hash.Hash
	switch algNorm {
	case "sha1":
		hashFn = sha1.New
	case "sha256":
		hashFn = sha256.New
	case "sha512":
		hashFn = sha512.New
	default:
		return nil, rpcErr(-32602, "algorithm must be one of sha1, sha256, sha512")
	}

	if (p.KeyText == nil) == (p.KeyBase64 == nil) {
		return nil, rpcErr(-32602, "host.crypto.hmac requires exactly one of key_text or key_base64")
	}
	if (p.DataText == nil) == (p.DataBase64 == nil) {
		return nil, rpcErr(-32602, "host.crypto.hmac requires exactly one of data_text or data_base64")
	}

	var key []byte
	if p.KeyBase64 != nil {
		decoded, err := base64.StdEncoding.DecodeString(*p.KeyBase64)
		if err != nil {
			return nil, rpcErr(-32602, "invalid key_base64: "+err.Error())
		}
		key = decoded
	} else {
		key = []byte(*p.KeyText)
	}

	var data []byte
	if p.DataBase64 != nil {
		decoded, err := base64.StdEncoding.DecodeString(*p.DataBase64)
		if err != nil {
			return nil, rpcErr(-32602, "invalid data_base64: "+err.Error())
		}
		data = decoded
	} else {
		data = []byte(*p.DataText)
	}

	mac := hmac.New(hashFn, key)
	_, _ = mac.Write(data)
	sum := mac.Sum(nil)
	return map[string]any{
		"algorithm": algNorm,
		"hex":       hex.EncodeToString(sum),
		"base64":    base64.StdEncoding.EncodeToString(sum),
	}, nil
}
