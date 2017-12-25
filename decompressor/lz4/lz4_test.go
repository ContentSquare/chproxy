package lz4

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func TestRead(t *testing.T) {
	testCases := []struct {
		compressed string
		expected   string
	}{
		{
			"\xfe\xf7\xd3-\xd9%\b\xb8\xeaz\xef\xe4Zt\xe8E\x823\x00\x00\x00+\x00\x00\x00\xf2\vSELECT number FROM system.\x13\x00\xb0s LIMIT 10\n",
			"SELECT number FROM system.numbers LIMIT 10\n",
		},
		{
			"\xbc\x83\xfb\x856BM\x82\xcdç\xa35\xd7\x18\xa7\x82(\x00\x00\x00\x1d\x00\x00\x00\xf0\x0eSELECT * FROM system.metrics\n",
			"SELECT * FROM system.metrics\n",
		},
		{
			"`\xf7d\xb6t\x9bWM\x15|{\x02\xc5s\xd0 \x82e\x00\x00\x00z\x00\x00\x00\xc0SELECT col1,\x06\x00\x112\x06\x00\x113\x06\x00\x114\x06\x00\x115\x06\x00\x116\x06\x00\x117\x06\x00\x118\x06\x00\x119\x06\x00\"10\a\x00\x02>\x00\x121?\x00\x121@\x00\x121A\x00\xf0\b15 FROM system.metrics\n",
			"SELECT col1, col2, col3, col4, col5, col6, col7, col8, col9, col10, col11, col12, col13, col14, col15 FROM system.metrics\n",
		},
		{
			"v\xad_\xfcX\x156C\xf6\xfb\xc9'\xb7\x8b\x11H\x82\v\x00\x00\x00\x01\x00\x00\x00\x10\n",
			"\n",
		},
	}

	for _, tc := range testCases {
		bb := bytes.NewBufferString(tc.compressed)
		r := NewReader(bb)
		b, err := ioutil.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.expected {
			t.Fatalf("got %q; expected: %q", string(b), tc.expected)
		}
	}
}

func TestReadNegative(t *testing.T) {
	testCases := []string{
		"",
		"\xfe",
		"\xfe\xf7\xd3-\xd9%\b\xb8\xeaz\xef\xe4Zt\xe8E\x823\x00\x00\x00+\x00",
		"\xfe\xf7\xd3-\xd9%\b\xb8\xeaz\xef\xe4Zt\xe8E\x823\x00\x00\x00+\x00\x00\x00\xf2\vSELECT number FROM system.\x13\x00\xb0s",
	}
	for _, v := range testCases {
		bb := bytes.NewBufferString(v)
		r := NewReader(bb)
		b := make([]byte, 128)
		_, err := r.Read(b)
		if err == nil {
			t.Fatalf("expected to get error for %q; got nil", v)
		}

	}
}
