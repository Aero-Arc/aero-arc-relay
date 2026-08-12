/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at http://mozilla.org/MPL/2.0/.
*/

package relay

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const bearerPrefix = "Bearer "

func newAgentTokenAuthenticator(tokens map[string]string) (func(context.Context, string) error, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	credentials := make(map[string]string, len(tokens))
	for agentID, token := range tokens {
		if agentID == "" || agentID != strings.TrimSpace(agentID) {
			return nil, errors.New("agent authentication contains an empty or untrimmed agent ID")
		}
		if token == "" || token != strings.TrimSpace(token) {
			return nil, fmt.Errorf("agent authentication token for %q is empty or has surrounding whitespace", agentID)
		}
		credentials[agentID] = token
	}
	return func(ctx context.Context, agentID string) error {
		meta, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "agent credentials are required")
		}
		values := meta.Get("authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], bearerPrefix) {
			return status.Error(codes.Unauthenticated, "agent credentials are required")
		}
		want, exists := credentials[agentID]
		got := strings.TrimPrefix(values[0], bearerPrefix)
		if !exists || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return status.Error(codes.Unauthenticated, "invalid agent credentials")
		}
		return nil
	}, nil
}

func (r *Relay) authenticateAgent(ctx context.Context, agentID string) error {
	if r.agentAuthenticator == nil {
		if r.registryReporter != nil {
			return status.Error(codes.Internal, "agent authentication is not configured")
		}
		return nil
	}
	return r.agentAuthenticator(ctx, agentID)
}
