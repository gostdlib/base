//go:build linux

package init

import (
	"testing"
)

func TestParseByteSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{
			name:  "Success: plain integer",
			input: "1073741824",
			want:  1073741824,
		},
		{
			name:  "Success: bytes suffix",
			input: "1024B",
			want:  1024,
		},
		{
			name:  "Success: KiB suffix",
			input: "100KiB",
			want:  100 * 1024,
		},
		{
			name:  "Success: MiB suffix",
			input: "256MiB",
			want:  256 * 1024 * 1024,
		},
		{
			name:  "Success: GiB suffix",
			input: "2GiB",
			want:  2 * 1024 * 1024 * 1024,
		},
		{
			name:  "Success: TiB suffix",
			input: "1TiB",
			want:  1024 * 1024 * 1024 * 1024,
		},
		{
			name:  "Success: whitespace trimmed",
			input: "  512MiB  ",
			want:  512 * 1024 * 1024,
		},
		{
			name:    "Error: invalid suffix",
			input:   "100XB",
			wantErr: true,
		},
		{
			name:    "Error: no number",
			input:   "MiB",
			wantErr: true,
		},
		{
			name:    "Error: empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, test := range tests {
		got, err := parseByteSize(test.input)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestParseByteSize(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestParseByteSize(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if got != test.want {
			t.Errorf("TestParseByteSize(%s): got %d, want %d", test.name, got, test.want)
		}
	}
}
