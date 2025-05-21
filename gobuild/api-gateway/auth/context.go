package auth

import (
	"context"
)

type contextKey string

const userClaimsContextKey contextKey = "userClaims"

func ContextWithUserClaims(ctx context.Context, claims *UserClaims) context.Context {
	return context.WithValue(ctx, userClaimsContextKey, claims)
}

func UserClaimsFromContext(ctx context.Context) (*UserClaims, bool) {
	claims, ok := ctx.Value(userClaimsContextKey).(*UserClaims)
	return claims, ok
}
