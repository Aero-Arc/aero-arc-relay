/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

package relay

import "errors"

var (
	ErrSessionNotFound        = errors.New("session not found")
	ErrCreatingTLSCredentials = errors.New("error creating TLS credentials")
	ErrCreatingTCPListener    = errors.New("error creating tcp listener")
	ErrGettingHomeDir         = errors.New("error getting user home directory")
)
