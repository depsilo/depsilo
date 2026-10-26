# Python offline acceptance fixture

This is a native-client acceptance tool for the planned offline distribution
feature. **Depsilo artifact export/import and offline activation are not shipped
by this fixture.** It accepts an already prepared target and never creates,
reconfigures, disconnects, or deletes that target.

The fixed profile is Linux/amd64, CPython 3.11.15 and pip 24.0 from the immutable
image in `profile.json`. The image digest is shared with `docker-pypi/Dockerfile`;
the Python and pip versions were checked inside that amd64 image. The historical
package versions are test inputs, not production recommendations.

`requirements.in` declares roots. `requirements.txt` pins the complete wheel
closure and SHA-256 hashes: Requests with its SOCKS extra, a native MarkupSafe
wheel, a Python-version marker, and a Windows-only dependency that must remain
absent on Linux. Requests hashes reuse the repository benchmark; the additional
hashes come from the corresponding PyPI release JSON records.

## Run

Acquire the client image before entering an isolated environment, or transfer it
through the environment's approved image-loading process:

```bash
docker pull --platform linux/amd64 \
  python:3.11-slim@sha256:db3ff2e1800a8581e2c48a27c3995339d47bdf046da21c7627accd3d51053a93
```

Prepare a dedicated target container with all fixture dependencies, connected
only to a Docker bridge created with `docker network create --internal`. No
other containers may be attached when the runner starts. The target's local
index must advertise only artifact URLs on the same origin. A future imported
dependency set should supply that index; warming an ordinary proxy cache does
not prove durable offline delivery.

```bash
python3 testground/offline-pypi/run.py \
  --target depsilo-offline-target --network depsilo-offline \
  --index-url http://depsilo-offline-target:23333/pypi/simple/ \
  --out /tmp/depsilo-offline-evidence-001
```

Host requirements: Python 3.9+ and Docker with `image inspect --platform`
support. The output directory must be new. The URL must name the target's
network alias/IP; credentials, query strings and fragments are rejected.
HTTP is permitted only for this explicitly selected local fixture target.

The runner checks target isolation before and after installation, probes TCP
egress from the target's network namespace, then creates a clean client with a
fresh virtual environment, no pip configuration, no client cache or proxies,
and hash-required wheel-only installation. It verifies the exact package set,
artifact origin, imports (including native code), markers and `pip check`.
The client also probes TCP egress. It records image identities, input hashes,
native pip evidence and logs in the output directory and removes only its own
temporary containers.

An internal Docker network plus failed probes is a test topology, not a security
certification against a hostile host or an administrator changing routing.
Do not attach gateway/proxy sidecars. The fixture does not establish that the
application never *attempted* egress, that imported provenance is trustworthy,
or that retention/import recovery works: the specification requires separate
server tests and request-log evidence for those claims. A successful run proves
only this pinned installation through the supplied target while disconnected.

## Offline guard tests

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s testground/offline-pypi -p 'test_*.py'
```

These tests exercise rejection of external/shared networks, unsafe target
addresses, missing dependencies and non-target artifacts without Docker or
internet access. They do not replace the native-client run.
