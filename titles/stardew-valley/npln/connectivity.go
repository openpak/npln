package npln

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/openpak/npln/internal/connectivity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func sharedICE(ctx context.Context) (*connectivity.ICE, error) {
	client, err := connectivity.FromEnvironment()
	if err != nil {
		return nil, status.Error(codes.Unavailable, "connectivity configuration unavailable")
	}
	if client == nil {
		return nil, nil
	}
	// callerUser can have a legacy uid-metadata development override. Shared
	// connectivity always requires the signed access bearer, even under that knob.
	_, uid, ok := bearer(ctx)
	if !ok || uid == "" {
		return nil, status.Error(codes.Unauthenticated, "verified bearer required for connectivity")
	}
	token := strings.TrimSpace(mdGet(ctx, "authorization"))
	token = strings.TrimPrefix(strings.TrimPrefix(token, "Bearer "), "bearer ")
	payload, ok := verifyJWT(token)
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if !ok || json.Unmarshal(payload, &claims) != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid connectivity identity")
	}
	ttl := min(3600, int(claims.Exp-time.Now().Unix()))
	if ttl < 1 {
		return nil, status.Error(codes.Unauthenticated, "expired connectivity identity")
	}
	digest := sha256.Sum256([]byte(token))
	country, _ := countryByUID.Load(uid)
	countryCode, _ := country.(string)
	// Tenant is the configured title, not caller-supplied tenant/user fields.
	key := connectivity.Identity{Title: lastSeg(Tenant), Session: "ice-bootstrap", Player: uid, Generation: hex.EncodeToString(digest[:]), Component: "pia-ice"}
	allocationCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ice, err := client.Allocate(allocationCtx, key, countryCode, ttl)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "shared connectivity allocation unavailable")
	}
	return &ice, nil
}
