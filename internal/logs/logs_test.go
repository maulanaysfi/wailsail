package logs

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPump(t *testing.T) {
	var got []string
	err := Pump(context.Background(), strings.NewReader("a\nb\nc"), 10*time.Millisecond,
		func(lines []string) { got = append(got, lines...) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("got %v", got)
	}
}

func TestPumpLongLines(t *testing.T) {
	exact := strings.Repeat("a", maxLine)
	over := strings.Repeat("b", maxLine+5)
	in := exact + "\n" + over + "\r\nnext\n"

	var got []string
	err := Pump(context.Background(), strings.NewReader(in), 10*time.Millisecond,
		func(lines []string) { got = append(got, lines...) })
	if err != nil {
		t.Fatalf("Pump returned error on >1 MiB line: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d lines, want 4", len(got))
	}
	if len(got[0]) != maxLine || got[0] != exact {
		t.Fatalf("line 0 len = %d, want %d", len(got[0]), maxLine)
	}
	if len(got[1]) != maxLine || got[2] != "bbbbb" || got[3] != "next" {
		t.Fatalf("unexpected split lines: len(1)=%d [2]=%q [3]=%q", len(got[1]), got[2], got[3])
	}
}
