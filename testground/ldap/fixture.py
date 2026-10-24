#!/usr/bin/env python3
"""An isolated TLS directory; generated credentials never enter the repository."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import socket
import ssl
import time

ROOT = Path(__file__).resolve().parent
STATE = Path.home() / ".cache" / "depsilo" / "ldap"
BASE = "dc=depsilo,dc=test"


def private(path, value):
    with path.open("x", encoding="utf-8") as handle:
        os.chmod(path, 0o600)
        handle.write(value)


def password_hash(password):
    salt = secrets.token_bytes(8)
    return "{SSHA}" + base64.b64encode(hashlib.sha1(password.encode() + salt).digest() + salt).decode()


def initialize():
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(STATE, 0o700)
    if (STATE / "credentials.json").exists():
        for name in ["directory.env", "certs/ldap.crt", "certs/ldap.key", "seed/50-users.ldif"]:
            if not (STATE / name).exists():
                raise RuntimeError("Incomplete LDAP fixture; preserve state and investigate")
        return
    if any(STATE.iterdir()):
        raise RuntimeError("Partial LDAP fixture; no existing state was overwritten")
    for name in ["certs", "seed", "database", "configuration", "runs"]:
        (STATE / name).mkdir(mode=0o700)
    credentials = {"url": "ldaps://127.0.0.1:18636", "starttls_url": "ldap://127.0.0.1:18389", "base_dn": BASE,
                   "bind_dn": f"cn=service,{BASE}", "bind_password": secrets.token_urlsafe(24), "users": {}}
    admin_password = secrets.token_urlsafe(32)
    private(STATE / "directory.env", "\n".join([
        "LDAP_ORGANISATION=Depsilo isolated directory", "LDAP_DOMAIN=depsilo.test", f"LDAP_BASE_DN={BASE}",
        f"LDAP_ADMIN_PASSWORD={admin_password}", f"LDAP_CONFIG_PASSWORD={secrets.token_urlsafe(32)}",
        "LDAP_TLS=true", "LDAP_TLS_ENFORCE=true", "LDAP_TLS_VERIFY_CLIENT=never",
        "LDAP_TLS_CRT_FILENAME=ldap.crt", "LDAP_TLS_KEY_FILENAME=ldap.key", "LDAP_TLS_CA_CRT_FILENAME=ldap.crt",
        "LDAP_REMOVE_CONFIG_AFTER_SETUP=false", "LDAP_LOG_LEVEL=0", ""]))
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "90",
                    "-keyout", str(STATE / "certs/ldap.key"), "-out", str(STATE / "certs/ldap.crt"),
                    "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    os.chmod(STATE / "certs/ldap.key", 0o600)
    entries = [f"dn: ou=people,{BASE}\nobjectClass: organizationalUnit\nou: people",
               f"dn: ou=groups,{BASE}\nobjectClass: organizationalUnit\nou: groups",
               f"dn: cn=service,{BASE}\nobjectClass: simpleSecurityObject\nobjectClass: organizationalRole\ncn: service\nuserPassword: {password_hash(credentials['bind_password'])}"]
    for name, group in [("admin-test", "admins"), ("reader-test", "readers"), ("unmapped-test", "unmapped")]:
        password = secrets.token_urlsafe(24)
        credentials["users"][name] = {"username": name, "password": password, "group": f"cn={group},ou=groups,{BASE}"}
        entries.append(f"dn: uid={name},ou=people,{BASE}\nobjectClass: inetOrgPerson\nuid: {name}\ncn: {name}\nsn: Test\ndisplayName: {name}\nuserPassword: {password_hash(password)}")
    for name, user in credentials["users"].items():
        group = user["group"].split(",")[0][3:]
        entries.append(f"dn: {user['group']}\nobjectClass: groupOfNames\ncn: {group}\nmember: uid={name},ou=people,{BASE}")
    private(STATE / "seed/50-users.ldif", "\n\n".join(entries) + "\n")
    private(STATE / "credentials.json", json.dumps(credentials, indent=2) + "\n")


def compose(*args, capture=False):
    return subprocess.run(["docker", "compose", "-f", str(ROOT / "compose.yaml"), *args],
                          env={**os.environ, "DEPSILO_LDAP_STATE": str(STATE)}, check=True,
                          stdout=subprocess.PIPE if capture else None, stderr=subprocess.PIPE if capture else None)


def ready():
    credentials = json.loads((STATE / "credentials.json").read_text())
    # Verify the published endpoint with the generated CA; macOS's system LDAP
    # client uses a different trust backend, so run the directory probe in Docker.
    context = ssl.create_default_context(cafile=str(STATE / "certs/ldap.crt"))
    with socket.create_connection(("127.0.0.1", 18636), timeout=3) as connection:
        with context.wrap_socket(connection, server_hostname="127.0.0.1"):
            pass
    response = subprocess.run(["docker", "exec", "-i", "depsilo-ldap-fixture-directory-1", "env",
                               "LDAPTLS_CACERT=/container/service/slapd/assets/certs/ldap.crt", "LDAPTLS_REQCERT=demand",
                               "ldapsearch", "-x", "-H", "ldaps://localhost:636", "-D", credentials["bind_dn"],
                               "-y", "/dev/stdin", "-b", "ou=people," + BASE, "(uid=admin-test)", "entryUUID", "memberOf"],
                              input=credentials["bind_password"].encode(), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
    return response.returncode == 0 and b"entryUUID:" in response.stdout and b"memberOf: cn=admins" in response.stdout


def grant_search():
    # The fixture's service account can read directory attributes, while the
    # preceding userPassword ACL continues to deny access to password hashes.
    command = ["docker", "exec", "-i", "depsilo-ldap-fixture-directory-1"]
    current = subprocess.run([*command, "ldapsearch", "-Q", "-Y", "EXTERNAL", "-H", "ldapi:///", "-LLL", "-o", "ldif-wrap=no",
                              "-b", "cn=config", f"(olcSuffix={BASE})", "olcAccess"], capture_output=True, timeout=5)
    if current.returncode != 0:
        return False
    text = current.stdout.decode()
    if f'by dn.exact="cn=service,{BASE}" read' in text:
        return True
    dn = next((line[4:] for line in text.splitlines() if line.startswith("dn: ")), None)
    if not dn:
        return False
    change = f'dn: {dn}\nchangetype: modify\nadd: olcAccess\nolcAccess: {{2}}to * by dn.exact="cn=service,{BASE}" read by * break\n'
    return subprocess.run([*command, "ldapmodify", "-Q", "-Y", "EXTERNAL", "-H", "ldapi:///"],
                          input=change.encode(), capture_output=True, timeout=5).returncode == 0


def configure_groups():
    command = ["docker", "exec", "-i", "depsilo-ldap-fixture-directory-1"]
    result = subprocess.run([*command, "ldapsearch", "-Q", "-Y", "EXTERNAL", "-H", "ldapi:///", "-LLL", "-o", "ldif-wrap=no",
                             "-b", "cn=config", "(objectClass=olcMemberOf)", "olcMemberOfGroupOC"], capture_output=True, timeout=5)
    text = result.stdout.decode()
    if "olcMemberOfGroupOC: groupOfNames" in text:
        return True
    dn = next((line[4:] for line in text.splitlines() if line.startswith("dn: ")), None)
    if not dn:
        return False
    change = f"dn: {dn}\nchangetype: modify\nreplace: olcMemberOfGroupOC\nolcMemberOfGroupOC: groupOfNames\n-\nreplace: olcMemberOfMemberAD\nolcMemberOfMemberAD: member\n"
    updated = subprocess.run([*command, "ldapmodify", "-Q", "-Y", "EXTERNAL", "-H", "ldapi:///"], input=change.encode(), capture_output=True, timeout=5)
    if updated.returncode != 0:
        return False
    # Populate the operational reverse attribute once after enabling the overlay.
    credentials = json.loads((STATE / "credentials.json").read_text())
    changes = []
    for name, user in credentials["users"].items():
        changes.append(f"dn: {user['group']}\nchangetype: modify\ndelete: member\nmember: uid={name},ou=people,{BASE}\n-\nadd: member\nmember: uid={name},ou=people,{BASE}\n")
    return subprocess.run([*command, "ldapmodify", "-Q", "-Y", "EXTERNAL", "-H", "ldapi:///"],
                          input="\n".join(changes).encode(), capture_output=True, timeout=5).returncode == 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=["start", "status", "stop"])
    args = parser.parse_args()
    if args.action == "start":
        initialize()
        compose("up", "-d", "directory")
        for attempt in range(60):
            try:
                if grant_search() and configure_groups() and ready():
                    print("LDAP fixture ready: verified LDAPS, service bind, UUID and group membership")
                    break
            except (subprocess.TimeoutExpired, OSError):
                pass
            time.sleep(1)
        else:
            raise SystemExit("LDAP readiness failed; state retained for diagnosis")
    elif args.action == "stop":
        compose("stop", "directory")
    else:
        compose("ps")
