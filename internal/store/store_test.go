package store_test

import (
	"context"
	"testing"

	"github.com/greatliontech/pbr/internal/store"
	"github.com/greatliontech/pbr/internal/store/mem"
)

func getStores() []store.Store {
	return []store.Store{
		mem.New(),
	}
}

func TestStore(t *testing.T) {
	ctx := context.Background()
	for _, s := range getStores() {

		testEmail := "test@testing.org"

		testUser, err := s.CreateUser(ctx, &store.User{
			Email: testEmail,
		})
		if err != nil {
			t.Fatal(err)
		}
		if testUser.Email != testEmail {
			t.Fatalf("expected email to be %q, got %q", testEmail, testUser.Email)
		}

		_, err = s.CreateUser(ctx, &store.User{
			Email: testEmail,
		})
		if err == nil {
			t.Fatalf("expected user with email %q to already exist", testEmail)
		}
		if err != store.ErrAlreadyExists {
			t.Fatalf("expected error to be %q, got %q", store.ErrAlreadyExists, err)
		}

		err = s.DeleteUser(ctx, testUser.Email)
		if err != nil {
			t.Fatalf("expected no error on DeleteUser, got %q", err)
		}

		err = s.DeleteUser(ctx, testUser.Email)
		if err == nil {
			t.Fatalf("expected error on DeleteUser for non-existent user")
		}
		if err != store.ErrNotFound {
			t.Fatalf("expected error to be %q, got %q", store.ErrNotFound, err)
		}
	}
}
