"""Native-client half of the disconnected Python acceptance fixture."""

import importlib.metadata
import json
import os
from pathlib import Path
import platform
import socket
import subprocess
import sys
from urllib.parse import urlsplit
import venv


def check_egress():
    # Paired with Docker internal-network inspection; these probes alone cannot
    # prove absence of every possible egress route.
    for address in ("1.1.1.1", "8.8.8.8"):
        try:
            connection = socket.create_connection((address, 443), timeout=2)
        except OSError:
            continue
        connection.close()
        raise RuntimeError(f"external TCP connectivity is available: {address}:443")


def origin(url):
    parsed = urlsplit(url)
    return parsed.scheme, parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80)


def check_report(report, profile, index_url):
    expected = {name.lower().replace("_", "-"): version for name, version in profile["packages"].items()}
    actual = {}
    for item in report["install"]:
        info = item["download_info"]
        if origin(info["url"]) != origin(index_url):
            raise RuntimeError("installation used an artifact outside the target origin")
        if not urlsplit(info["url"]).path.endswith(".whl"):
            raise RuntimeError("installation did not use a wheel")
        if not info.get("archive_info", {}).get("hashes", {}).get("sha256"):
            raise RuntimeError("installation report is missing a wheel SHA-256")
        metadata = item["metadata"]
        name = metadata["name"].lower().replace("_", "-")
        if name in actual:
            raise RuntimeError("duplicate distribution in installation report")
        actual[name] = metadata["version"]
    if actual != expected:
        raise RuntimeError(f"installed package closure differs: {actual!r}")
    return actual


def main():
    if sys.argv[1:] == ["--egress-only"]:
        check_egress()
        return
    fixture = Path(__file__).resolve().parent
    profile = json.loads((fixture / "profile.json").read_text())
    index_url = sys.argv[1]
    result_path = Path("/tmp/offline-pypi-result.json")
    result = {"status": "failed", "scope": "disconnected-native-install-only"}
    try:
        if platform.system() != "Linux" or platform.machine() != "x86_64":
            raise RuntimeError("fixture requires Linux/amd64")
        if platform.python_version() != profile["python_version"]:
            raise RuntimeError("Python version does not match the pinned fixture")
        if importlib.metadata.version("pip") != profile["pip_version"]:
            raise RuntimeError("pip version does not match the pinned fixture")
        check_egress()
        # A new container plus a new venv eliminates installed dependencies.
        # Explicit env removes image/host proxies, extra indexes and pip options.
        environment = {
            "PATH": "/usr/local/bin:/usr/bin:/bin",
            "HOME": "/tmp/offline-pypi-home",
            "PIP_CONFIG_FILE": os.devnull,
            "PYTHONNOUSERSITE": "1",
            "LANG": "C.UTF-8",
        }
        venv_path = Path("/tmp/offline-pypi-venv")
        if venv_path.exists():
            raise RuntimeError("fixture requires a fresh installation directory")
        venv.create(venv_path, with_pip=True)
        python = str(venv_path / "bin/python")
        command = [
            python, "-m", "pip", "--disable-pip-version-check", "--no-cache-dir",
            "install", "--no-input", "--require-hashes", "--only-binary=:all:",
            "--index-url", index_url, "--retries", "0", "--timeout", "10",
            "--report", "/tmp/offline-pypi-install.json",
            "-r", str(fixture / "requirements.txt"),
        ]
        if urlsplit(index_url).scheme == "http":
            # Only the Docker-local target is trusted; the runner validates it.
            command.extend(["--trusted-host", urlsplit(index_url).hostname])
        subprocess.run(command, env=environment, check=True, timeout=180)
        report = json.loads(Path("/tmp/offline-pypi-install.json").read_text())
        installed = check_report(report, profile, index_url)
        subprocess.run([python, "-m", "pip", "check"], env=environment, check=True, timeout=30)
        subprocess.run([
            python, "-c",
            "import requests, socks, typing_extensions; "
            "from markupsafe import _speedups; "
            "from importlib.util import find_spec; "
            "assert find_spec('colorama') is None; "
            "assert requests.__version__ == '2.32.3'; "
            "print('pure Python, native wheel, extra and markers: OK')",
        ], env=environment, check=True, timeout=30)
        result.update(status="passed", packages=installed, python=platform.python_version(),
                      pip=profile["pip_version"], target=index_url, install_report=report)
    except Exception as error:
        result["error"] = str(error)
        raise
    finally:
        result_path.write_text(json.dumps(result, indent=2) + "\n")


if __name__ == "__main__":
    main()
