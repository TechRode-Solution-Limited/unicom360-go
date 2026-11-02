# Security policy

## Reporting a vulnerability

Please report security problems privately, never in a public issue or pull request.

* **GitHub:** use private vulnerability reporting on this repository (the "Security" tab, then
  "Report a vulnerability").
* **Email:** info@techrode.co, with "Security" in the subject.

Tell us what is affected, how to reproduce it, and what an attacker could do with it. We
acknowledge reports within three working days and keep you informed until it is fixed.

## Scope

This repository is the Go SDK for the Unicom360 API. Problems such as a leaked API key or
webhook secret in logs or errors, a webhook signature check that can be bypassed, or a retry
that can send a message twice belong here. Problems in the Unicom360 API itself can be sent to
the same address.

The webhook secrets in `testdata/` and in the examples are throwaway test values, not
credentials.

## Supported versions

Only the latest release receives fixes.
