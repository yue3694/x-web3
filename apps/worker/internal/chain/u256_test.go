package chain_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x-web3/worker/internal/chain"
)

func TestBigIntToU256(t *testing.T) {
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)) // 2^256 - 1
	overflow := new(big.Int).Lsh(big.NewInt(1), 256)                               // 2^256
	neg := big.NewInt(-1)

	cases := []struct {
		name    string
		in      *big.Int
		wantHex string
		wantErr bool
	}{
		{name: "nil → zero", in: nil, wantHex: strings.Repeat("00", 32)},
		{name: "zero", in: big.NewInt(0), wantHex: strings.Repeat("00", 32)},
		{name: "one", in: big.NewInt(1), wantHex: "0000000000000000000000000000000000000000000000000000000000000001"},
		{name: "1e18", in: big.NewInt(1_000_000_000_000_000_000), wantHex: "0000000000000000000000000000000000000000000000000de0b6b3a7640000"},
		{name: "max u256", in: max, wantHex: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
		{name: "overflow", in: overflow, wantErr: true},
		{name: "negative", in: neg, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := chain.BigIntToU256(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			gotHex := ""
			for _, b := range out {
				gotHex += hexByte(b)
			}
			if gotHex != tc.wantHex {
				t.Fatalf("hex mismatch:\n  got  %s\n  want %s", gotHex, tc.wantHex)
			}
		})
	}
}

func TestU256FromUint64(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0000000000000000000000000000000000000000000000000000000000000000"},
		{1, "0000000000000000000000000000000000000000000000000000000000000001"},
		{1_000_000, "00000000000000000000000000000000000000000000000000000000000f4240"},
		{^uint64(0), "000000000000000000000000000000000000000000000000ffffffffffffffff"},
	}
	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			out := chain.U256FromUint64(tc.in)
			gotHex := ""
			for _, b := range out {
				gotHex += hexByte(b)
			}
			if gotHex != tc.want {
				t.Fatalf("hex mismatch:\n  got  %s\n  want %s", gotHex, tc.want)
			}
		})
	}
}

func TestBytes16UUIDRoundTrip(t *testing.T) {
	id := uuid.MustParse("01020304-0506-0708-0910-111213141516")
	b := chain.Bytes16FromUUID(id)
	got := chain.Bytes16ToUUID(b)
	if got != id {
		t.Fatalf("round-trip mismatch: got %s want %s", got, id)
	}
}

func TestHexToBytes32(t *testing.T) {
	// 64 hex chars = 32 bytes
	good := strings.Repeat("ab", 32)
	out, err := chain.HexToBytes32(good)
	if err != nil {
		t.Fatalf("HexToBytes32(64-char): %v", err)
	}
	for i, b := range out {
		if b != 0xab {
			t.Errorf("byte %d: got %x want ab", i, b)
		}
	}

	cases := []string{
		"",                                 // 空
		strings.Repeat("ab", 31),           // 短 1 byte
		strings.Repeat("ab", 33),           // 长 1 byte
		"0x" + good,                        // 含 0x 前缀
		strings.Repeat("zz", 32),           // 非 hex 字符
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := chain.HexToBytes32(in); err == nil {
				t.Fatalf("expected error for input %q", in)
			}
		})
	}
}

func hexByte(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&0x0f]})
}
