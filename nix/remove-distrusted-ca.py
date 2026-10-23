"""Remove only the affected NSS certificate and trust objects before building."""
import re
import sys
from pathlib import Path

# NSS CKA_LABEL values, not X.509 common names.
BLOCKED_LABELS = {
    "emSign ECC Root CA - G3",
    "Comodo AAA Services root",  # Subject: AAA Certificate Services
}


def filter_certdata(data):
    # NSS objects are separated by blank lines; octal values contain no blanks.
    blocks = re.split(r"(\n[ \t]*\n)", data)
    removed = []
    for index, block in enumerate(blocks):
        label = re.search(r'^CKA_LABEL UTF8 "([^"]+)"$', block, re.MULTILINE)
        if label and label.group(1) in BLOCKED_LABELS:
            if not re.search(
                r"^CKA_CLASS CK_OBJECT_CLASS CKO_(CERTIFICATE|NSS_TRUST)$",
                block, re.MULTILINE,
            ):
                raise ValueError("Unexpected NSS object for blocked CA")
            removed.append(label.group(1))
            blocks[index] = ""
    return "".join(blocks), removed


def main():
    path = Path(sys.argv[1])
    filtered, removed = filter_certdata(path.read_text(encoding="utf-8"))
    path.write_text(filtered, encoding="utf-8")
    print(f"Removed {len(removed)} certificate/trust objects: {sorted(set(removed))}")


if __name__ == "__main__":
    main()
