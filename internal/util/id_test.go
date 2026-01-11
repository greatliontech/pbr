package util

import (
	"testing"
)

func TestOwnerID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantSame bool // if true, same input should produce same output
	}{
		{
			name:     "simple owner name",
			input:    "testowner",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "empty string",
			input:    "",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "owner with special characters",
			input:    "owner-with-dashes_and_underscores",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "unicode owner",
			input:    "ownerWithUnicode",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "long owner name",
			input:    "this-is-a-very-long-owner-name-that-exceeds-typical-lengths",
			wantLen:  32,
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OwnerID(tt.input)

			// Check length
			if len(got) != tt.wantLen {
				t.Errorf("OwnerID(%q) length = %d, want %d", tt.input, len(got), tt.wantLen)
			}

			// Check consistency
			if tt.wantSame {
				got2 := OwnerID(tt.input)
				if got != got2 {
					t.Errorf("OwnerID(%q) not consistent: got %q, then %q", tt.input, got, got2)
				}
			}

			// Check it's valid hex
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("OwnerID(%q) = %q contains non-hex character %c", tt.input, got, c)
					break
				}
			}
		})
	}
}

func TestOwnerID_DifferentInputsDifferentOutputs(t *testing.T) {
	id1 := OwnerID("owner1")
	id2 := OwnerID("owner2")

	if id1 == id2 {
		t.Errorf("OwnerID(owner1) == OwnerID(owner2), expected different values")
	}
}

func TestModuleID(t *testing.T) {
	tests := []struct {
		name     string
		ownerID  string
		modName  string
		wantLen  int
		wantSame bool
	}{
		{
			name:     "simple module",
			ownerID:  "abc123",
			modName:  "mymodule",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "empty module name",
			ownerID:  "ownerid",
			modName:  "",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "empty owner id",
			ownerID:  "",
			modName:  "module",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "both empty",
			ownerID:  "",
			modName:  "",
			wantLen:  32,
			wantSame: true,
		},
		{
			name:     "module with special characters",
			ownerID:  "owner123",
			modName:  "module-with-dashes",
			wantLen:  32,
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ModuleID(tt.ownerID, tt.modName)

			// Check length
			if len(got) != tt.wantLen {
				t.Errorf("ModuleID(%q, %q) length = %d, want %d", tt.ownerID, tt.modName, len(got), tt.wantLen)
			}

			// Check consistency
			if tt.wantSame {
				got2 := ModuleID(tt.ownerID, tt.modName)
				if got != got2 {
					t.Errorf("ModuleID(%q, %q) not consistent: got %q, then %q", tt.ownerID, tt.modName, got, got2)
				}
			}

			// Check it's valid hex
			for _, c := range got {
				if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
					t.Errorf("ModuleID(%q, %q) = %q contains non-hex character %c", tt.ownerID, tt.modName, got, c)
					break
				}
			}
		})
	}
}

func TestModuleID_DifferentInputsDifferentOutputs(t *testing.T) {
	ownerID := "sameowner"

	id1 := ModuleID(ownerID, "module1")
	id2 := ModuleID(ownerID, "module2")

	if id1 == id2 {
		t.Errorf("ModuleID with different module names should produce different IDs")
	}

	id3 := ModuleID("owner1", "module")
	id4 := ModuleID("owner2", "module")

	if id3 == id4 {
		t.Errorf("ModuleID with different owner IDs should produce different IDs")
	}
}

func TestModuleID_CombinationUniqueness(t *testing.T) {
	// Test that "a/bc" and "ab/c" produce different IDs
	id1 := ModuleID("a", "bc")
	id2 := ModuleID("ab", "c")

	if id1 == id2 {
		t.Errorf("ModuleID(a, bc) == ModuleID(ab, c), expected different due to separator")
	}
}

func TestFakeUUID(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "simple string", input: "test"},
		{name: "long string", input: "this is a very long string that should still produce a 32 character output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fakeUUID(tt.input)

			// Should be 32 characters
			if len(got) != 32 {
				t.Errorf("fakeUUID(%q) length = %d, want 32", tt.input, len(got))
			}

			// Should be deterministic
			got2 := fakeUUID(tt.input)
			if got != got2 {
				t.Errorf("fakeUUID(%q) not deterministic: got %q, then %q", tt.input, got, got2)
			}
		})
	}
}

func BenchmarkOwnerID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		OwnerID("testowner")
	}
}

func BenchmarkModuleID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ModuleID("abc123def456", "testmodule")
	}
}
