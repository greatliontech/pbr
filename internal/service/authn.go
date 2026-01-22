package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AuthnService implements the buf.alpha.registry.v1alpha1.AuthnService interface.
// It provides authentication helpers for buf CLI operations like login verification.
type AuthnService struct {
	svc *Service
}

// NewAuthnService creates a new AuthnService.
func NewAuthnService(svc *Service) *AuthnService {
	return &AuthnService{svc: svc}
}

// GetCurrentUser gets information associated with the current user.
// The user's ID is retrieved from the request's authentication header.
func (a *AuthnService) GetCurrentUser(
	ctx context.Context,
	req *connect.Request[registryv1alpha1.GetCurrentUserRequest],
) (*connect.Response[registryv1alpha1.GetCurrentUserResponse], error) {
	username := userFromContext(ctx)
	if username == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// Generate a deterministic user ID from the username
	userID := generateUserID(username)

	user := &registryv1alpha1.User{
		Id:                 userID,
		Username:           username,
		CreateTime:         timestamppb.Now(),
		UpdateTime:         timestamppb.Now(),
		Deactivated:        false,
		VerificationStatus: registryv1alpha1.VerificationStatus_VERIFICATION_STATUS_UNSPECIFIED,
		UserType:           registryv1alpha1.UserType_USER_TYPE_PERSONAL,
	}

	return connect.NewResponse(&registryv1alpha1.GetCurrentUserResponse{
		User: user,
	}), nil
}

// GetCurrentUserSubject gets the currently logged in user's subject.
// The subject is a unique identifier for mapping to an identity provider.
func (a *AuthnService) GetCurrentUserSubject(
	ctx context.Context,
	req *connect.Request[registryv1alpha1.GetCurrentUserSubjectRequest],
) (*connect.Response[registryv1alpha1.GetCurrentUserSubjectResponse], error) {
	username := userFromContext(ctx)
	if username == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}

	// The subject is typically a unique identifier from the identity provider.
	// For simple username/password auth, we use the username as the subject.
	// For OIDC, this would be the sub claim from the ID token.
	subject := username

	return connect.NewResponse(&registryv1alpha1.GetCurrentUserSubjectResponse{
		Subject: subject,
	}), nil
}

// generateUserID creates a deterministic user ID from a username.
func generateUserID(username string) string {
	hash := sha256.Sum256([]byte(username))
	return hex.EncodeToString(hash[:16])
}
