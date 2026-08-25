package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/makinje/aero-arc-relay/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type controlPlaneAuthorizer func(context.Context) error

func newControlPlaneAuthorizer(cfg config.ControlAuthConfig) (controlPlaneAuthorizer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.ClientCAFile == "" {
		return nil, errors.New("control-plane client CA file is required")
	}
	if len(cfg.AllowedIdentities) == 0 {
		return nil, errors.New("at least one control-plane identity is required")
	}
	allowed := make(map[string]struct{}, len(cfg.AllowedIdentities))
	for _, identity := range cfg.AllowedIdentities {
		if identity == "" {
			return nil, errors.New("control-plane identity cannot be empty")
		}
		allowed[identity] = struct{}{}
	}
	return func(ctx context.Context) error {
		remote, ok := peer.FromContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "verified control-plane client certificate is required")
		}
		tlsInfo, ok := remote.AuthInfo.(credentials.TLSInfo)
		if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
			return status.Error(codes.Unauthenticated, "verified control-plane client certificate is required")
		}
		leaf := tlsInfo.State.PeerCertificates[0]
		if certificateIdentityAllowed(leaf, allowed) {
			return nil
		}
		return status.Error(codes.PermissionDenied, "control-plane client identity is not authorized")
	}, nil
}

func certificateIdentityAllowed(certificate *x509.Certificate, allowed map[string]struct{}) bool {
	if certificate == nil {
		return false
	}
	identities := make([]string, 0, 1+len(certificate.DNSNames)+len(certificate.URIs))
	identities = append(identities, certificate.Subject.CommonName)
	identities = append(identities, certificate.DNSNames...)
	for _, uri := range certificate.URIs {
		identities = append(identities, uri.String())
	}
	for _, identity := range identities {
		if _, ok := allowed[identity]; ok {
			return true
		}
	}
	return false
}

func serverTransportCredentials(cfg *config.Config, certificatePath, keyPath string) (credentials.TransportCredentials, error) {
	certificate, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
	}
	if cfg.ControlAuth.Enabled {
		contents, err := os.ReadFile(cfg.ControlAuth.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read control-plane client CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(contents) {
			return nil, errors.New("control-plane client CA contains no certificates")
		}
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		tlsConfig.ClientCAs = roots
	}
	return credentials.NewTLS(tlsConfig), nil
}
