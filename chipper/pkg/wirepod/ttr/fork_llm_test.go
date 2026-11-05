package wirepod_ttr

import (
	"reflect"
	"testing"
)

func TestForkFlushUnpunctuated(t *testing.T) {
	cases := []struct {
		name    string
		chunks  []string
		tail    string
		want    []string
		flushed bool
	}{
		{"bare command", nil, " {{getImage||front}} ", []string{"{{getImage||front}}"}, true},
		{"sentences already split", []string{"Hi."}, " more", []string{"Hi."}, false},
		{"empty reply", nil, "  ", nil, false},
	}
	for _, c := range cases {
		got, flushed := forkFlushUnpunctuated(c.chunks, c.tail)
		if flushed != c.flushed || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, got, flushed, c.want, c.flushed)
		}
	}
}
