package arch

import (
	"encoding/hex"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// IntentEvent is a parsed DIA_ORACLE.* log line.
type IntentEvent struct {
	Kind         string // "update" | "stale" | "rejected"
	IntentHash   [32]byte
	Symbol       string
	Signer       EthAddress
	Price        *big.Int // nil for rejected
	Timestamp    uint64   // 0 for rejected
	StaleAgainst uint64   // 0 unless Kind == "stale"
	Reason       string   // "" unless Kind == "rejected"
}

const programLogPrefix = "Program log: "

var (
	keyRegex     = regexp.MustCompile(`(\w+)=("[^"]*"|0x[0-9a-fA-F]+|\d+|\w+)`)
	hexValRegex  = regexp.MustCompile(`^0x([0-9a-fA-F]+)$`)
	quotedRegex  = regexp.MustCompile(`^"(.*)"$`)
)

// ParseIntentEvents scans a flat slice of validator log strings and returns
// parsed DIA_ORACLE.INTENT_* events in order. Non-matching lines are skipped.
func ParseIntentEvents(logs []string) []IntentEvent {
	var out []IntentEvent
	for _, line := range logs {
		body := strings.TrimPrefix(line, programLogPrefix)
		switch {
		case strings.HasPrefix(body, "DIA_ORACLE.INTENT_UPDATE "):
			if e, ok := parseUpdate(body); ok {
				out = append(out, e)
			}
		case strings.HasPrefix(body, "DIA_ORACLE.INTENT_STALE "):
			if e, ok := parseStale(body); ok {
				out = append(out, e)
			}
		case strings.HasPrefix(body, "DIA_ORACLE.INTENT_REJECTED "):
			if e, ok := parseRejected(body); ok {
				out = append(out, e)
			}
		}
	}
	return out
}

func parseKV(body string) map[string]string {
	out := map[string]string{}
	for _, m := range keyRegex.FindAllStringSubmatch(body, -1) {
		key, val := m[1], m[2]
		if q := quotedRegex.FindStringSubmatch(val); q != nil {
			val = q[1]
		}
		out[key] = val
	}
	return out
}

func parseUpdate(body string) (IntentEvent, bool) {
	kv := parseKV(body)
	e := IntentEvent{Kind: "update", Symbol: kv["symbol"]}
	if !decodeHexInto32(kv["intent_hash"], &e.IntentHash) {
		return e, false
	}
	if !decodeHexInto20(kv["signer"], (*[20]byte)(&e.Signer)) {
		return e, false
	}
	if p, ok := new(big.Int).SetString(kv["price"], 10); ok {
		e.Price = p
	}
	if ts, err := strconv.ParseUint(kv["timestamp"], 10, 64); err == nil {
		e.Timestamp = ts
	}
	return e, true
}

func parseStale(body string) (IntentEvent, bool) {
	kv := parseKV(body)
	e := IntentEvent{Kind: "stale", Symbol: kv["symbol"]}
	if !decodeHexInto32(kv["intent_hash"], &e.IntentHash) {
		return e, false
	}
	if !decodeHexInto20(kv["signer"], (*[20]byte)(&e.Signer)) {
		return e, false
	}
	if p, ok := new(big.Int).SetString(kv["price"], 10); ok {
		e.Price = p
	}
	if ts, err := strconv.ParseUint(kv["timestamp"], 10, 64); err == nil {
		e.Timestamp = ts
	}
	if ets, err := strconv.ParseUint(kv["existing_timestamp"], 10, 64); err == nil {
		e.StaleAgainst = ets
	}
	return e, true
}

func parseRejected(body string) (IntentEvent, bool) {
	kv := parseKV(body)
	e := IntentEvent{Kind: "rejected", Symbol: kv["symbol"], Reason: kv["reason"]}
	if !decodeHexInto32(kv["intent_hash"], &e.IntentHash) {
		return e, false
	}
	if !decodeHexInto20(kv["signer"], (*[20]byte)(&e.Signer)) {
		return e, false
	}
	return e, true
}

func decodeHexInto32(s string, out *[32]byte) bool {
	m := hexValRegex.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	raw, err := hex.DecodeString(m[1])
	if err != nil || len(raw) != 32 {
		return false
	}
	copy(out[:], raw)
	return true
}

func decodeHexInto20(s string, out *[20]byte) bool {
	m := hexValRegex.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	raw, err := hex.DecodeString(m[1])
	if err != nil || len(raw) != 20 {
		return false
	}
	copy(out[:], raw)
	return true
}
