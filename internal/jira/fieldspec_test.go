package jira

import (
	"testing"
)

func TestParseFieldSpec(t *testing.T) {
	tests := []struct {
		token   string
		want    FieldSpec
		wantErr bool
	}{
		{
			token: "labels+=foo",
			want:  FieldSpec{Name: "labels", Op: OpAdd, Raw: "foo"},
		},
		{
			token: "labels-=foo",
			want:  FieldSpec{Name: "labels", Op: OpRemove, Raw: "foo"},
		},
		{
			token: "summary=hello world",
			want:  FieldSpec{Name: "summary", Op: OpReplace, Raw: "hello world"},
		},
		{
			token: "priority=High",
			want:  FieldSpec{Name: "priority", Op: OpReplace, Raw: "High"},
		},
		{
			// += is detected before = so "labels+=" with empty value is valid
			token: "labels+=",
			want:  FieldSpec{Name: "labels", Op: OpAdd, Raw: ""},
		},
		{
			// @ prefix in value is preserved verbatim
			token: "description=@/tmp/desc.md",
			want:  FieldSpec{Name: "description", Op: OpReplace, Raw: "@/tmp/desc.md"},
		},
		{
			// @ prefix with += is also preserved
			token: "description+=@/tmp/extra.md",
			want:  FieldSpec{Name: "description", Op: OpAdd, Raw: "@/tmp/extra.md"},
		},
		{
			// value itself contains "=" — only the first "=" is the operator
			token: "summary=a=b",
			want:  FieldSpec{Name: "summary", Op: OpReplace, Raw: "a=b"},
		},
		{
			// += takes priority over a later "=" — "name" is "x", raw is "y=z"
			token: "x+=y=z",
			want:  FieldSpec{Name: "x", Op: OpAdd, Raw: "y=z"},
		},
		{
			// no operator → error
			token:   "justalabel",
			wantErr: true,
		},
		{
			// empty string → error
			token:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			got, err := ParseFieldSpec(tc.token)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFieldSpec(%q) expected error, got nil", tc.token)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFieldSpec(%q) unexpected error: %v", tc.token, err)
			}
			if got.Name != tc.want.Name {
				t.Errorf("Name: got %q, want %q", got.Name, tc.want.Name)
			}
			if got.Op != tc.want.Op {
				t.Errorf("Op: got %d, want %d", got.Op, tc.want.Op)
			}
			if got.Raw != tc.want.Raw {
				t.Errorf("Raw: got %q, want %q", got.Raw, tc.want.Raw)
			}
		})
	}
}
