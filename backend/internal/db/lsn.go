package db

import (
	"fmt"
	"strconv"
	"strings"
)

// LSN is a position in the write ahead log. A replica whose replay LSN is past a
// user's last write is fresh enough to serve that user.
type LSN uint64

// ParseLSN parses the textual pg_lsn form, "XXXXXXXX/XXXXXXXX", into an LSN.
func ParseLSN(s string) (LSN, error) {
	hi, lo, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		return 0, fmt.Errorf("malformed lsn %q", s)
	}
	h, err := strconv.ParseUint(hi, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed lsn %q: %w", s, err)
	}
	l, err := strconv.ParseUint(lo, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed lsn %q: %w", s, err)
	}
	return LSN(h<<32 | l), nil
}

// String renders the LSN in the textual pg_lsn form.
func (l LSN) String() string {
	return fmt.Sprintf("%X/%08X", uint64(l)>>32, uint64(l)&0xFFFFFFFF)
}
