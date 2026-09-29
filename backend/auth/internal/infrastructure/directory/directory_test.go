package directory

import (
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestIsUniqueViolation(t *testing.T) {
	if !isUniqueViolation(status.Error(codes.AlreadyExists, "already exists")) {
		t.Fatal("typed conflict lost")
	}
	if isUniqueViolation(status.Error(codes.Internal, "duplicate key connection error")) {
		t.Fatal("text classified as conflict")
	}
}

func TestDummyHashIsValid(t *testing.T) {
	dummyHash := mustDummyHash()
	if _, err := bcrypt.Cost(dummyHash); err != nil {
		t.Fatalf("dummy hash is not a valid bcrypt hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(dummyHash, []byte("anything")); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("want mismatch against dummy hash, got %v", err)
	}
}
