#!/usr/bin/env python3
"""Run a fixed native client against an already prepared, isolated target."""

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit
import uuid

FIXTURE = Path(__file__).resolve().parent


def docker(*args, **kwargs):
    return subprocess.run(["docker", *args], check=True, text=True, **kwargs)


def inspect(kind, name, *extra):
    return json.loads(docker(kind, "inspect", *extra, name, capture_output=True).stdout)[0]


def validate_target(target, network, index_url):
    if not target["State"].get("Running"):
        raise ValueError("target container must be running")
    host = target["HostConfig"]
    if host.get("Privileged") or host.get("NetworkMode", "").startswith(("host", "container:")):
        raise ValueError("privileged or shared/host network targets are not supported")
    if any(cap.upper().removeprefix("CAP_") in {"ALL", "NET_ADMIN", "SYS_ADMIN"}
           for cap in host.get("CapAdd") or []):
        raise ValueError("target can change network isolation")
    if network.get("Driver") != "bridge" or not network.get("Internal"):
        raise ValueError("target must use a dedicated Docker internal bridge network")
    connections = target["NetworkSettings"]["Networks"]
    if len(connections) != 1 or next(iter(connections.values()))["NetworkID"] != network["Id"]:
        raise ValueError("target must be attached only to the selected internal network")
    if set(network.get("Containers", {})) != {target["Id"]}:
        raise ValueError("internal network must contain only the target before this run")
    endpoint = next(iter(connections.values()))
    allowed_hosts = set(endpoint.get("Aliases") or []) | set(endpoint.get("DNSNames") or [])
    allowed_hosts.update((endpoint.get("IPAddress"), endpoint.get("GlobalIPv6Address")))
    url = urlsplit(index_url)
    if (url.scheme not in {"http", "https"} or not url.hostname or url.hostname not in allowed_hosts
            or url.username is not None or url.password is not None or url.query or url.fragment
            or not url.path.endswith("/simple/")):
        raise ValueError("index URL must name the target's network IP/alias and end with /simple/")
    # Reject invalid ports before any client is started.
    _ = url.port


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True, help="prepared target container name or ID")
    parser.add_argument("--network", required=True, help="dedicated internal bridge network")
    parser.add_argument("--index-url", required=True, help="target's local /simple/ endpoint")
    parser.add_argument("--out", required=True, type=Path, help="new report directory")
    args = parser.parse_args()
    profile = json.loads((FIXTURE / "profile.json").read_text())
    # Never pull during acceptance: image acquisition is a separate online step.
    image = inspect("image", profile["client_image"], "--platform", profile["platform"])
    if image.get("Os") != "linux" or image.get("Architecture") != "amd64":
        raise ValueError("preload the fixture image for linux/amd64")
    target = inspect("container", args.target)
    network = inspect("network", args.network)
    validate_target(target, network, args.index_url)
    args.out.mkdir(parents=True, exist_ok=False)
    provenance = {
        "status": "running", "scope": "disconnected-native-install-only",
        "target_container": target["Id"], "target_image": target["Image"],
        "network_id": network["Id"], "network_internal": True,
        "client_image": profile["client_image"], "client_image_id": image["Id"],
        "platform": profile["platform"], "index_url": args.index_url,
        "inputs": {name: hashlib.sha256((FIXTURE / name).read_bytes()).hexdigest()
                   for name in ("requirements.in", "requirements.txt", "profile.json", "client.py")},
    }
    report_path = args.out / "run.json"
    report_path.write_text(json.dumps(provenance, indent=2) + "\n")
    client_name = "depsilo-offline-pypi-" + uuid.uuid4().hex[:12]
    probe_name = client_name + "-egress"
    client_created = False
    try:
        # A temporary helper uses the target's network namespace to test its
        # egress, without changing or installing anything inside the target.
        with (args.out / "egress.log").open("w") as log:
            docker("run", "--rm", "--name", probe_name, "--pull=never", "--platform", profile["platform"],
                   "--network", "container:" + target["Id"], "--cap-drop=ALL",
                   "--security-opt=no-new-privileges", "--entrypoint=python",
                   profile["client_image"], "-c", (FIXTURE / "client.py").read_text(),
                   "--egress-only", stdout=log, stderr=subprocess.STDOUT, timeout=30)
        docker("create", "--name", client_name, "--pull=never", "--platform", profile["platform"],
               "--network", network["Id"], "--cap-drop=ALL", "--security-opt=no-new-privileges",
               "--entrypoint=python", profile["client_image"], "/fixture/client.py", args.index_url,
               stdout=subprocess.DEVNULL)
        client_created = True
        # docker cp also works with a remote daemon; no host bind mounts needed.
        docker("cp", str(FIXTURE) + "/.", client_name + ":/fixture")
        with (args.out / "client.log").open("w") as log:
            subprocess.run(["docker", "start", "--attach", client_name], check=False,
                           stdout=log, stderr=subprocess.STDOUT, timeout=240)
        exit_code = inspect("container", client_name)["State"]["ExitCode"]
        docker("cp", client_name + ":/tmp/offline-pypi-result.json", str(args.out / "client.json"))
        client_result = json.loads((args.out / "client.json").read_text())
        if exit_code != 0 or client_result.get("status") != "passed":
            raise RuntimeError("native install failed; inspect client.log and client.json")
        # Detect changes to target isolation during the run. The client is
        # removed first so validate_target can require a single network member.
        docker("rm", client_name, stdout=subprocess.DEVNULL)
        client_created = False
        validate_target(inspect("container", args.target), inspect("network", args.network), args.index_url)
        provenance["status"] = "passed"
    except Exception as error:
        provenance.update(status="failed", error=str(error))
        raise
    finally:
        report_path.write_text(json.dumps(provenance, indent=2) + "\n")
        subprocess.run(["docker", "rm", "--force", probe_name], stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, check=False)
        if client_created:
            subprocess.run(["docker", "rm", "--force", client_name], stdout=subprocess.DEVNULL, check=False)
    print(f"Disconnected native install passed; evidence: {args.out}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, RuntimeError, subprocess.SubprocessError, OSError) as error:
        print(f"offline-pypi: {error}", file=sys.stderr)
        sys.exit(1)
