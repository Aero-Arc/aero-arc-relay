package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"

	"github.com/makinje/aero-arc-relay/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestControlPlaneAuthorizerRequiresVerifiedAllowedIdentity(t *testing.T) {
	authorize, err := newControlPlaneAuthorizer(config.ControlAuthConfig{
		Enabled: true, ClientCAFile: "unused-by-authorizer.pem", AllowedIdentities: []string{"spiffe://aero-arc/api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := authorize(context.Background()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing certificate error = %v, want Unauthenticated", err)
	}

	certificate := &x509.Certificate{
		Subject: pkix.Name{CommonName: "not-api"},
		URIs:    mustParseURIs(t, "spiffe://aero-arc/api"),
	}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}}})
	if err := authorize(ctx); err != nil {
		t.Fatalf("allowed URI identity rejected: %v", err)
	}

	certificate.URIs = mustParseURIs(t, "spiffe://aero-arc/other")
	if err := authorize(ctx); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong identity error = %v, want PermissionDenied", err)
	}
}

func mustParseURIs(t *testing.T, values ...string) []*url.URL {
	t.Helper()
	result := make([]*url.URL, 0, len(values))
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, parsed)
	}
	return result
}
