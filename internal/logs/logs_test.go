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
