package model

import "testing"

func TestUint64UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		want      uint64
		wantValid bool
		wantErr   bool
	}{
		{name: "empty", input: []byte(""), wantValid: false},
		{name: "null", input: []byte(" null "), wantValid: false},
		{name: "empty string", input: []byte(`"  "`), wantValid: false},
		{name: "quoted", input: []byte(`"42"`), want: 42, wantValid: true},
		{name: "number", input: []byte(`7`), want: 7, wantValid: true},
		{name: "bad quoted json", input: []byte(`"`), wantErr: true},
		{name: "bad quoted number", input: []byte(`"nope"`), wantErr: true},
		{name: "bad number", input: []byte(`{}`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Uint64
			err := got.UnmarshalJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Value != tt.want || got.Valid != tt.wantValid {
				t.Fatalf("unexpected value: %#v", got)
			}
		})
	}
}

func TestTimestampUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		want      int64
		wantValid bool
		wantErr   bool
	}{
		{name: "empty", input: []byte(""), wantValid: false},
		{name: "null", input: []byte(" null "), wantValid: false},
		{name: "empty string", input: []byte(`"  "`), wantValid: false},
		{name: "quoted", input: []byte(`"1700000000"`), want: 1_700_000_000, wantValid: true},
		{name: "number", input: []byte(`-1`), want: -1, wantValid: true},
		{name: "bad quoted json", input: []byte(`"`), wantErr: true},
		{name: "bad quoted number", input: []byte(`"nope"`), wantErr: true},
		{name: "bad number", input: []byte(`{}`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Timestamp
			err := got.UnmarshalJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Value != tt.want || got.Valid != tt.wantValid {
				t.Fatalf("unexpected value: %#v", got)
			}
		})
	}
}
