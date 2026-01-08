package relay

import "errors"

var (
	ErrSessionNotFound        = errors.New("session not found")
	ErrCreatingTLSCredentials = errors.New("error creating TLS credentials")
	ErrCreatingTCPListener    = errors.New("error creating tcp listener")
	ErrGettingHomeDir         = errors.New("error getting user home directory")
)
