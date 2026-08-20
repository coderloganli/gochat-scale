package password

import (
	"strings"
	"testing"
)

func TestHashIsNotPlaintext(t *testing.T) {
	const plain = "correct horse battery staple"
	hash, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if hash == plain || strings.Contains(hash, plain) {
		t.Fatal("hash leaks the plaintext password")
	}
	if !strings.HasPrefix(hash, "$2a$") {
		t.Fatalf("hash is not a bcrypt hash: %q", hash)
	}
}

func TestHashIsSaltedPerCall(t *testing.T) {
	const plain = "same-password"
	first, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	second, err := Hash(plain)
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	if first == second {
		t.Fatal("identical passwords produced identical hashes, salt is missing")
	}
	if !Verify(first, plain) || !Verify(second, plain) {
		t.Fatal("both hashes should verify against the original password")
	}
}

func TestVerify(t *testing.T) {
	hash, err := Hash("s3cret")
	if err != nil {
		t.Fatalf("Hash returned error: %v", err)
	}
	cases := []struct {
		name  string
		plain string
		want  bool
	}{
		{"correct", "s3cret", true},
		{"wrong", "s3cre", false},
		{"empty", "", false},
		{"case_differs", "S3cret", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Verify(hash, tc.plain); got != tc.want {
				t.Errorf("Verify(%q) = %v, want %v", tc.plain, got, tc.want)
			}
		})
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	// A row still holding a legacy plaintext password must never authenticate.
	if Verify("plaintext", "plaintext") {
		t.Fatal("a plaintext value must not be accepted as a valid hash")
	}
}

func TestHashRejectsOverlongPassword(t *testing.T) {
	long := strings.Repeat("a", MaxLength+1)
	if _, err := Hash(long); err == nil {
		t.Fatal("expected an error for a password longer than bcrypt accepts")
	}
}
