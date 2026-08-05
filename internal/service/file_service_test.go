package service

import "testing"

func TestValidateFolderName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"alphanumeric", "Certificates", false},
		{"with spaces", "My Folder", false},
		{"with punctuation", "Work-2026.backup", false},
		{"trims surrounding whitespace", "   Padded   ", false},
		{"rejects slash", "a/b", true},
		{"rejects backslash", `a\b`, true},
		{"rejects newline", "a\nb", true},
		{"rejects cr", "a\rb", true},
		{"rejects null", "a\x00b", true},
		{"rejects too long", string(make([]byte, 256)), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFolderName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateFolderName(%q) err=%v, wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}
