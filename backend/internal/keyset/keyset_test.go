package keyset

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestACursorComesBackAsItWent(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 30, 45, 123456000, time.FixedZone("CEST", 2*60*60))
	id := uuid.New()
	got, err := Decode(Cursor{At: at, ID: id}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if !got.At.Equal(at) || got.ID != id {
		t.Errorf("decoded %v %v, want %v %v", got.At, got.ID, at, id)
	}
}

func TestNoCursorIsTheStart(t *testing.T) {
	got, err := Decode("")
	if err != nil || got != nil {
		t.Fatalf("Decode(\"\") = %v, %v", got, err)
	}
	at, id := got.Args()
	if at != nil || id != nil {
		t.Errorf("the start binds %v %v, want nulls", at, id)
	}
}

func TestACursorNotWrittenHereIsRefused(t *testing.T) {
	for _, text := range []string{"%%%", "bm90aGluZw", "eHx5", "MjAyNi0xMC0wMVQxMjowMDowMFp8bm90LWEtdXVpZA"} {
		if _, err := Decode(text); !errors.Is(err, ErrBadCursor) {
			t.Errorf("Decode(%q) = %v, want ErrBadCursor", text, err)
		}
	}
}

func TestNextIsOnlyWhenMoreWasRead(t *testing.T) {
	last := Cursor{At: time.Now(), ID: uuid.New()}
	if got := Next(20, 20, last); got != nil {
		t.Errorf("a full window with nothing after has a next cursor %q", *got)
	}
	got := Next(20, 21, last)
	if got == nil {
		t.Fatal("a window with a row more has no next cursor")
	}
	if back, err := Decode(*got); err != nil || back.ID != last.ID {
		t.Errorf("the next cursor reads back as %v, %v", back, err)
	}
}
