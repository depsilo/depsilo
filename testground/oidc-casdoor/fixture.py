#!/usr/bin/env python3
"""Manage one isolated, loopback-only Casdoor interoperability fixture."""

import argparse
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import urllib.request


DIRECTORY = Path(__file__).resolve().parent
STATE = Path.home() / ".cache" / "depsilo" / "oidc-casdoor"
ISSUER = "http://127.0.0.1:18000"
ORGANIZATION = "depsilo-test"
APPLICATION = "depsilo-oidc-test"
IMAGE = "casbin/casdoor-all-in-one:4.23.0@sha256:0a65b3cbca95ec16783e43eb7520f152de16e1eaa9b6b6547f01d7d006da6da6"
CALLBACKS = [
    "http://127.0.0.1:18081/api/v1/auth/oidc/callback",
    "http://127.0.0.1:18082/callback",
]


def write_private(path, content):
    with path.open("x", encoding="utf-8") as handle:
        os.chmod(path, 0o600)
        handle.write(content)


def initialize():
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(STATE, 0o700)
    credentials_path = STATE / "credentials.json"
    required = [credentials_path, STATE / "init_data.json", STATE / "app.conf"]
    if all(path.is_file() for path in required):
        for path in required:
            os.chmod(path, 0o600)
        return
    if any(path.exists() for path in required) or (STATE / "casdoor.db").exists():
        raise RuntimeError("Incomplete fixture state; preserve it and investigate before retrying")

    (STATE / "logs").mkdir(mode=0o700, exist_ok=True)
    credentials = {
        "issuer": ISSUER,
        "organization": ORGANIZATION,
        "application": APPLICATION,
        "client_id": secrets.token_hex(16),
        "client_secret": secrets.token_urlsafe(32),
        "redirect_uris": CALLBACKS,
        "console": {
            "organization": "built-in",
            "username": "admin",
            "password": secrets.token_urlsafe(24),
        },
        "users": {},
    }
    users = []
    for name, group, forbidden in [
        ("admin-test", "depsilo-admins", False),
        ("reader-test", "depsilo-readers", False),
        ("unmapped-test", "unmapped", False),
        ("disabled-test", "depsilo-readers", True),
    ]:
        password = secrets.token_urlsafe(24)
        credentials["users"][name] = {
            "username": name,
            "password": password,
            "groups": [f"{ORGANIZATION}/{group}"],
            "forbidden": forbidden,
        }
        users.append({
            "owner": ORGANIZATION, "name": name, "type": "normal-user",
            "displayName": name, "password": password,
            "email": f"{name}@example.invalid", "emailVerified": True,
            "isAdmin": False, "isForbidden": forbidden, "isDeleted": False,
            "signupApplication": APPLICATION,
            "groups": [f"{ORGANIZATION}/{group}"],
        })
    seed = {
        "organizations": [{
            "owner": "admin", "name": ORGANIZATION,
            "displayName": "Depsilo isolated OIDC test",
            "passwordType": "bcrypt", "passwordOptions": ["AtLeast6"],
            "languages": ["en", "zh"], "countryCodes": ["US"],
            "defaultApplication": APPLICATION,
            "defaultTokenFormat": "JWT-Standard", "isProfilePublic": False,
        }],
        "certs": [{
            "owner": "admin", "name": "depsilo-test-cert",
            "displayName": "Local test signing certificate", "scope": "JWT",
            "type": "x509", "cryptoAlgorithm": "RS256", "bitSize": 2048,
            "expireInYears": 1,
        }],
        "applications": [{
            "owner": "admin", "name": APPLICATION,
            "displayName": "Depsilo OIDC protocol fixture",
            "organization": ORGANIZATION, "cert": "depsilo-test-cert",
            "category": "Default", "type": "Web",
            "enablePassword": True, "enableSignUp": False,
            "enableSigninSession": False, "disableSignin": False,
            "enableAutoSignin": False, "enableGuestSignin": False,
            "clientId": credentials["client_id"],
            "clientSecret": credentials["client_secret"],
            "providers": [], "signinMethods": [
                {"name": "Password", "displayName": "Password", "rule": "All"},
            ],
            "signupItems": [
                {"name": "ID", "visible": False, "required": True, "rule": "Random"},
                {"name": "Username", "visible": True, "required": True, "rule": "None"},
                {"name": "Password", "visible": True, "required": True, "rule": "None"},
            ],
            "grantTypes": ["authorization_code"], "redirectUris": CALLBACKS,
            "tokenFormat": "JWT-Standard", "tokenGroupFormat": "ID",
            "tokenFields": [], "expireInHours": 1, "cookieExpireInHours": 1,
            "failedSigninLimit": 10, "failedSigninFrozenTime": 1,
        }],
        "groups": [{
            "owner": ORGANIZATION, "name": group, "displayName": group,
            "type": "Virtual", "isTopGroup": True, "isEnabled": True,
        } for group in ["depsilo-admins", "depsilo-readers", "unmapped"]],
        "users": users,
    }
    config = f"""appname = casdoor
httpport = 8000
runmode = prod
copyrequestbody = true
driverName = sqlite
dataSourceName = file:/fixture/casdoor.db?cache=shared&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)
dbName = casdoor
showSql = false
origin = {ISSUER}
originFrontend = {ISSUER}
staticBaseUrl = https://cdn.casbin.org
isDemoMode = false
authState = {secrets.token_urlsafe(24)}
initAdminPassword = {credentials['console']['password']}
initDataFile = /fixture/init_data.json
initDataNewOnly = true
initDataWatchInterval = 0
defaultLanguage = en
logConfig = {{"adapter":"file","filename":"/fixture/logs/casdoor.log","maxdays":3,"perm":"0600"}}
"""
    write_private(credentials_path, json.dumps(credentials, indent=2) + "\n")
    write_private(STATE / "init_data.json", json.dumps(seed, indent=2) + "\n")
    write_private(STATE / "app.conf", config)


def compose(*args, capture=False):
    endpoint = subprocess.check_output(
        ["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"],
        text=True,
    ).strip()
    if not endpoint.startswith("unix://") or os.environ.get("DOCKER_HOST"):
        raise RuntimeError("This fixture requires a local Unix-socket Docker context without DOCKER_HOST")
    env = dict(os.environ, DEPSILO_CASDOOR_STATE=str(STATE))
    return subprocess.run(
        ["docker", "compose", "--project-name", "depsilo-oidc-casdoor", "-f",
         str(DIRECTORY / "compose.yaml"), *args],
        env=env, check=True, text=True, capture_output=capture,
    )


class RejectRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


def get_json(url):
    # The fixture must contact its loopback listener directly, even when the
    # developer's shell or macOS system settings have an HTTP proxy configured.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), RejectRedirects())
    with opener.open(url, timeout=5) as response:
        return json.load(response)


def metadata():
    discovery = get_json(ISSUER + "/.well-known/openid-configuration")
    if discovery.get("issuer") != ISSUER:
        raise RuntimeError("Discovery issuer does not match the configured loopback issuer")
    if "S256" not in discovery.get("code_challenge_methods_supported", []):
        raise RuntimeError("The provider does not advertise S256 PKCE")
    for key in ["authorization_endpoint", "token_endpoint", "userinfo_endpoint", "jwks_uri"]:
        if not discovery.get(key, "").startswith(ISSUER + "/"):
            raise RuntimeError(f"Unexpected provider endpoint: {key}")
    keys = get_json(discovery["jwks_uri"])
    app_keys = get_json(ISSUER + "/.well-known/" + APPLICATION + "/jwks")
    if not keys.get("keys") or not app_keys.get("keys"):
        raise RuntimeError("Provider signing keys are missing")
    report = {
        "issuer": ISSUER, "application": APPLICATION, "image": IMAGE,
        "source_tag": "v4.23.0",
        "source_commit": "1b91d9d319a127e9b1af88fa7a20bb35c052b9d8",
        "discovery": discovery,
        "jwks": keys, "application_jwks": app_keys,
        "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    path = STATE / "metadata.json"
    path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    os.chmod(path, 0o600)
    print(f"Discovery issuer, S256 and signing keys checked. Metadata: {path}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["start", "stop", "status", "metadata", "paths"])
    args = parser.parse_args()
    os.umask(0o077)
    if args.command == "start":
        initialize()
        compose("up", "-d", "--wait", "--wait-timeout", "120")
        metadata()
    elif args.command == "stop":
        compose("down")
        print("Fixture stopped. Private SQLite data and credentials were preserved.")
    elif args.command == "status":
        compose("ps")
    elif args.command == "metadata":
        metadata()
    if args.command in ["start", "paths"]:
        print(f"Casdoor: {ISSUER}")
        print(f"Private credentials: {STATE / 'credentials.json'}")
        print(f"Private runtime state: {STATE}")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, subprocess.CalledProcessError, OSError, ValueError) as error:
        print(f"Casdoor fixture: {error}", file=sys.stderr)
        sys.exit(1)
