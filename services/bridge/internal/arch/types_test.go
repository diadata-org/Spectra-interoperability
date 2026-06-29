package arch

import (
	"math/big"
	"testing"
)

func TestU256FromBigInt(t *testing.T) {
	cases := []struct {
		name string
		in   *big.Int
		want string // hex
	}{
		{"zero", big.NewInt(0), "0000000000000000000000000000000000000000000000000000000000000000"},
		{"one", big.NewInt(1), "0000000000000000000000000000000000000000000000000000000000000001"},
		{"max64", new(big.Int).SetUint64(^uint64(0)), "000000000000000000000000000000000000000000000000ffffffffffffffff"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := U256FromBigInt(c.in)
			if hex := bytesToHex(got[:]); hex != c.want {
				t.Fatalf("U256FromBigInt(%s) = %s; want %s", c.in, hex, c.want)
			}
		})
	}
}

func TestU256RoundTrip(t *testing.T) {
	for _, raw := range []string{"00", "01", "ff", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"} {
		bi, ok := new(big.Int).SetString(raw, 16)
		if !ok {
			t.Fatalf("bad hex %s", raw)
		}
		got := BigIntFromU256(U256FromBigInt(bi))
		if got.Cmp(bi) != 0 {
			t.Fatalf("round-trip failed: %s -> %s", bi, got)
		}
	}
}

func bytesToHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[i*2] = hexdigits[x>>4]
		out[i*2+1] = hexdigits[x&0x0f]
	}
	return string(out)
}
