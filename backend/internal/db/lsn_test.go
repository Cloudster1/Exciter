package db

import "testing"

func TestParseLSN(t *testing.T) {
	tests := []struct {
		in      string
		want    LSN
		wantErr bool
	}{
		{in: "0/0", want: 0},
		{in: "0/16B3748", want: 0x16B3748},
		{in: "1/0", want: 1 << 32},
		{in: "  2/A1B2C3D4  ", want: 2<<32 | 0xA1B2C3D4},
		{in: "FFFFFFFF/FFFFFFFF", want: ^LSN(0)},
		{in: "nonsense", wantErr: true},
		{in: "0/", wantErr: true},
		{in: "/0", wantErr: true},
		{in: "100000000/0", wantErr: true}, // high half overflows 32 bits
	}
	for _, tt := range tests {
		got, err := ParseLSN(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseLSN(%q) = %v, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLSN(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseLSN(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestLSNStringRoundTrip(t *testing.T) {
	for _, in := range []string{"0/00000000", "0/016B3748", "1/00000000", "2/A1B2C3D4"} {
		l, err := ParseLSN(in)
		if err != nil {
			t.Fatalf("ParseLSN(%q): %v", in, err)
		}
		if got := l.String(); got != in {
			t.Errorf("round trip of %q produced %q", in, got)
		}
	}
}
