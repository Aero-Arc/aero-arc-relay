# Contributing to Aero Arc Relay

Thank you for contributing to Aero Arc Relay. Bug reports, documentation
improvements, tests, and code changes are all welcome.

## Before You Start

- Search the existing issues and pull requests to avoid duplicating work.
- Open an issue before making a large or architectural change so the approach
  can be discussed first.
- Keep each pull request focused on one change.

## Development Setup

You will need:

- Git
- Go 1.24 or newer
- Any external services required by the sink or integration you are changing

Clone your fork and download the dependencies:

```sh
git clone https://github.com/YOUR-USERNAME/aero-arc-relay.git
cd aero-arc-relay
go mod download
```

Build the relay:

```sh
make build
```

## Making Changes

Create a branch from the latest default branch:

```sh
git switch -c your-change-name
```

When changing Go code:

- Follow standard Go conventions and keep the code formatted with `gofmt`.
- Add or update tests for behavior changes and bug fixes.
- Preserve existing public APIs unless the change intentionally requires a
  breaking change.
- Add the repository's MPL 2.0 copyright header to new Go files.
- Do not commit secrets, credentials, generated coverage reports, or local
  build artifacts.

## Testing and Quality Checks

Run the unit tests:

```sh
make test
```

Format and vet the code:

```sh
make fmt
make vet
```

For broader changes, also run:

```sh
make test-race
make lint
```

Changes involving a cloud or external-service sink should include focused unit
tests. If manual integration testing is required, describe the environment and
results in the pull request without including credentials.

## Pull Requests

Before opening a pull request:

- Rebase or merge the latest default branch into your branch.
- Confirm the relevant tests and quality checks pass.
- Update documentation and configuration examples when behavior changes.
- Explain what changed, why it changed, and how it was tested.
- Link any related issues.
- Call out breaking changes, operational impact, or follow-up work.

Review feedback is part of the contribution process. Please keep discussion
constructive and update the pull request as needed.

## Licensing

Aero Arc Relay is licensed under the Mozilla Public License 2.0. By submitting
a contribution, you agree that your contribution will be licensed under the
same terms. See [LICENSE](LICENSE) for the complete license text.
