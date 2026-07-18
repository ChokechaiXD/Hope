package cortex

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"strings"
	"unicode"
)

// EmbedDim is the fixed dimensionality of the OFFLINE hashing embedder.
// The remote embedder (see embed_http.go) uses RemoteEmbedDim (2048) and is
// preferred when configured; this constant only governs the fallback path.
const EmbedDim = 256

// embedOffline converts free text into a fixed-length unit vector using a
// deterministic hashing scheme (feature hashing of unigrams + bigrams). This is
// the fallback used only when the remote embedder (9Router) is unavailable. It
// catches paraphrase/synonym overlap without any network call.
func embedOffline(text string) []float64 {
	vec := make([]float64, EmbedDim)
	tokens := embedTokens(text)
	// Unigram features.
	for _, tok := range tokens {
		hashFeature(vec, tok, 1.0)
	}
	// Bigram features capture short phrase structure (paraphrase signal).
	for i := 1; i < len(tokens); i++ {
		hashFeature(vec, tokens[i-1]+" "+tokens[i], 0.5)
	}
	// Tag-style tokens (#word) get extra weight as strong topical anchors.
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "#") && len(tok) > 1 {
			hashFeature(vec, tok, 1.0)
		}
	}
	normalize(vec)
	return vec
}

// embedTokens lowercases, NFKC-folds, strips combining marks, and splits on
// non-letter/non-digit boundaries. Thai (no spaces) is handled by also emitting
// character bigrams when a token is longer than a threshold.
func embedTokens(text string) []string {
	normalized := foldString(strings.ToLower(text))
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '#')
	})
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
		// Thai/Lao/CJK runs have no spaces; emit 2-char sliding windows.
		if len([]rune(p)) > 4 && hasUnspacedScript(p) {
			runesP := []rune(p)
			for i := 0; i+2 <= len(runesP); i++ {
				out = append(out, string(runesP[i:i+2]))
			}
		}
	}
	if len(out) == 0 {
		out = []string{"<empty>"}
	}
	return out
}

// foldString applies a lightweight NFKC-ish normalization: maps fullwidth and
// compatibility forms approximately and removes combining marks so accented
// variants of a word land in the same bucket. Stdlib-only, no external deps.
func foldString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E: // fullwidth ASCII variants
			r = r - 0xFF01 + 0x21
		case unicode.Is(unicode.Mn, r): // combining mark -> drop
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hasUnspacedScript(s string) bool {
	for _, r := range s {
		if r >= 0x0E00 && r <= 0x0E7F { // Thai
			return true
		}
		if r >= 0x3400 && r <= 0x9FFF { // CJK
			return true
		}
	}
	return false
}

// hashFeature maps a token to two bucket indices via a 64-bit hash split, and
// adds a signed weight (sign from the hash) so collisions partially cancel.
func hashFeature(vec []float64, token string, weight float64) {
	h := sha256.Sum256([]byte(token))
	primary := binary.BigEndian.Uint32(h[0:4]) % uint32(len(vec))
	secondary := binary.BigEndian.Uint32(h[4:8]) % uint32(len(vec))
	sign := 1.0
	if h[8]%2 == 1 {
		sign = -1.0
	}
	vec[primary] += weight * sign
	vec[secondary] += weight * 0.5 * sign
}

func normalize(vec []float64) {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return
	}
	for i := range vec {
		vec[i] /= norm
	}
}

// encodeVector packs a float64 vector into a compact little-endian BLOB.
func encodeVector(vec []float64) []byte {
	buf := make([]byte, len(vec)*8)
	for i, v := range vec {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(v))
	}
	return buf
}

// decodeVector reverses encodeVector. Returns nil on malformed input.
func decodeVector(buf []byte) []float64 {
	if len(buf) == 0 || len(buf)%8 != 0 {
		return nil
	}
	n := len(buf) / 8
	vec := make([]float64, n)
	for i := 0; i < n; i++ {
		vec[i] = math.Float64frombits(binary.LittleEndian.Uint64(buf[i*8:]))
	}
	return vec
}

// cosineSimilarity returns the cosine of two equal-length vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	if dot <= 0 {
		return 0
	}
	return dot
}

// EmbedTextForBackfill exposes the offline embedder to the CLI backfill command
// so existing memories get the exact same vectors used at write time.
func EmbedTextForBackfill(text string) []float64 { return embedText(text) }

// EncodeVectorForBackfill packs a vector for storage; pair of EmbedTextForBackfill.
func EncodeVectorForBackfill(vec []float64) []byte { return encodeVector(vec) }
