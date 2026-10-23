"""Check the complete CA set, including the paths installed in OCI images."""
import re
import subprocess
import sys
from pathlib import Path

BLOCKED = {
    # emSign ECC Root CA - G3
    "86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b",
    # AAA Certificate Services, the alternate cross-sign trust anchor
    "d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4",
}


def fingerprints(path):
    data = Path(path).read_bytes()
    certificates = re.findall(
        rb"-----BEGIN (?:TRUSTED )?CERTIFICATE-----.*?"
        rb"-----END (?:TRUSTED )?CERTIFICATE-----", data, re.DOTALL
    )
    if not certificates:
        raise AssertionError(f"Empty CA bundle: {path}")
    result = set()
    for certificate in certificates:
        output = subprocess.check_output(
            ["openssl", "x509", "-noout", "-fingerprint", "-sha256"],
            input=certificate,
        ).decode()
        result.add(output.strip().split("=")[1].replace(":", "").lower())
    return result


def main():
    original_path, *updated_paths = sys.argv[1:]
    if not updated_paths:
        raise SystemExit("Usage: check-ca-trust.py ORIGINAL UPDATED [UPDATED ...]")
    original = fingerprints(original_path)
    expected = original - BLOCKED
    for path in updated_paths:
        actual = fingerprints(path)
        assert not actual & BLOCKED, f"Distrusted root remains in {path}"
        assert actual == expected, (
            f"Unexpected CA changes in {path}: "
            f"missing={expected - actual}, added={actual - expected}"
        )
        print(f"{path}: {len(actual)} roots; blocked roots absent, others unchanged")


if __name__ == "__main__":
    main()

