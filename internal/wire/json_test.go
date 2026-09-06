package wire

import (
	"strings"
	"testing"
)

func TestStrictJSON(t *testing.T) {
	bad := [][]byte{[]byte(`{"a":1,"\u0061":2}`), []byte(`{} {}`), []byte(`{"a":"\ud800"}`), []byte(`{"a":"\udfff"}`), []byte{34, 255, 34}, []byte(strings.Repeat("[", 100) + strings.Repeat("]", 100)), []byte(`[NaN]`)}
	for _, b := range bad {
		if Validate(b) == nil {
			t.Errorf("accepted %q", b)
		}
	}
	for _, s := range []string{`{"x":"\ud83d\ude00"}`, `{"a":[1,true,null]}`, `{"x":"\\ud800"}`} {
		if e := Validate([]byte(s)); e != nil {
			t.Error(s, e)
		}
	}
}
func FuzzValidate(f *testing.F) {
	f.Add([]byte(`{"x":[1,2]}`))
	f.Add([]byte(`{"a":1,"a":2}`))
	f.Fuzz(func(t *testing.T, b []byte) { _ = Validate(b) })
}
