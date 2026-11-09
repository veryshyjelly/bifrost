"""Bifrost transfer helper: executed in an isolated Docker container."""
import hashlib
import json
import os
import sqlite3
import sys
import tarfile
import tempfile
from pathlib import Path, PurePosixPath

FILES = {"bridge.env", "telegram-api.env", "data.tar.gz", "media.tar.gz", "telegram-data.tar.gz"}


class TransferError(ValueError):
    pass


def safe_members(archive, outer=False):
    seen = set()
    for member in archive.getmembers():
        path = PurePosixPath(member.name)
        if path.is_absolute() or ".." in path.parts or "\\" in member.name or not (member.isfile() or member.isdir()):
            raise TransferError("Unsafe archive entry")
        name = str(path)
        if name == "." and member.isdir():
            continue
        if name in seen:
            raise TransferError("Duplicate archive entry")
        seen.add(name)
        if outer and (name not in FILES | {"manifest.json"} or not member.isfile()):
            raise TransferError("Unexpected export entry")
    if outer and seen != FILES | {"manifest.json"}:
        raise TransferError("Incomplete export")


def sha256(path):
    with open(path, "rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def extract(archive, destination):
    archive.extractall(destination, filter="data")


def main():
    operation, *args = sys.argv[1:]
    if operation == "export":
        for name in ["data", "media", "telegram-data"]:
            print(f"Exporting {name} volume", flush=True)
            with tarfile.open(f"/transfer/{name}.tar.gz", "w:gz", dereference=False) as archive:
                archive.add(f"/volumes/{name}", arcname=".")
            os.chmod(f"/transfer/{name}.tar.gz", 0o600)
            os.chown(f"/transfer/{name}.tar.gz", int(os.environ["TRANSFER_UID"]), int(os.environ["TRANSFER_GID"]))
        # Read the frozen database copy, including WAL if present, rather than
        # guessing account identity from a possibly rotated connection setting.
        with tempfile.TemporaryDirectory(dir="/transfer") as directory:
            with tarfile.open("/transfer/data.tar.gz", "r:gz") as archive:
                safe_members(archive)
                extract(archive, directory)
            db = sqlite3.connect(f"file:{directory}/bridge.db?mode=ro", uri=True)
            row = db.execute("SELECT value FROM kv WHERE kind='identity' AND id='account'").fetchone()
            db.close()
            Path("/transfer/account.json").write_text(row[0] if row else "{}")
    elif operation == "unpack":
        with tarfile.open("/export.tar.gz", "r:gz") as archive:
            safe_members(archive, outer=True)
            extract(archive, "/transfer")
        manifest = json.loads(Path("/transfer/manifest.json").read_text())
        if manifest.get("format") != "bifrost" or manifest.get("version") != 1 or set(manifest.get("files", {})) != FILES:
            raise TransferError("Unsupported export manifest")
        for name in FILES:
            if sha256(f"/transfer/{name}") != manifest["files"][name]:
                raise TransferError("Export checksum mismatch")
        for name in ["data", "media", "telegram-data"]:
            with tarfile.open(f"/transfer/{name}.tar.gz", "r:gz") as archive:
                safe_members(archive)
                if name == "data":
                    destination = Path("/transfer/check-data")
                    destination.mkdir()
                    extract(archive, destination)
        db = sqlite3.connect("file:/transfer/check-data/bridge.db?mode=ro", uri=True)
        if db.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
            raise TransferError("Invalid bridge database")
        required = {"kv", "jobs", "effects", "messages", "aliases", "telegram_messages"}
        if not required.issubset({row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}):
            raise TransferError("Incomplete bridge database")
        db.close()
        for path in Path("/transfer").rglob("*"):
            if path.is_file():
                os.chmod(path, 0o600)
            os.chown(path, int(os.environ["TRANSFER_UID"]), int(os.environ["TRANSFER_GID"]))
    elif operation == "restore":
        for name in ["data", "media", "telegram-data"]:
            if any(Path(f"/volumes/{name}").iterdir()):
                raise TransferError("Refusing nonempty destination volume")
        for name in ["data", "media", "telegram-data"]:
            print(f"Restoring {name} volume", flush=True)
            with tarfile.open(f"/transfer/{name}.tar.gz", "r:gz") as archive:
                safe_members(archive)
                extract(archive, f"/volumes/{name}")
            # tarfile's data filter deliberately discards archived uid/gid. These
            # application volumes belong to the non-root users in our pinned images.
            for path in [Path(f"/volumes/{name}"), *Path(f"/volumes/{name}").rglob("*")]:
                os.chown(path, 1000, 1000)
    else:
        raise TransferError("Unknown transfer operation")


if __name__ == "__main__":
    try:
        main()
    except TransferError as error:
        print(f"Bifrost transfer rejected: {error}", file=sys.stderr)
        sys.exit(1)
    except Exception:
        print("Bifrost transfer rejected: invalid archive or inaccessible state", file=sys.stderr)
        sys.exit(1)
