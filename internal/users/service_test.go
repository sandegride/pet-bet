package users

import "testing"

func TestIsAdminUsesConfiguredTelegramIDsOnly(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, nil, 1000, []int64{123, 456})

	isAdmin, err := service.IsAdmin(t.Context(), 123)
	if err != nil {
		t.Fatalf("IsAdmin() error = %v", err)
	}
	if !isAdmin {
		t.Fatal("configured telegram id must be admin")
	}

	isAdmin, err = service.IsAdmin(t.Context(), 789)
	if err != nil {
		t.Fatalf("IsAdmin() error = %v", err)
	}
	if isAdmin {
		t.Fatal("non-configured telegram id must not be admin")
	}
}
